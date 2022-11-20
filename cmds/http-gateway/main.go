package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"syscall"
	"time"

	"cloud.google.com/go/profiler"
	"github.com/interuss/dss/pkg/api/v1/auxpb"
	"github.com/interuss/dss/pkg/api/v1/ridpbv1"
	"github.com/interuss/dss/pkg/api/v1/scdpb"
	"github.com/interuss/dss/pkg/api/v2/ridpbv2"
	"github.com/interuss/dss/pkg/build"
	"github.com/interuss/dss/pkg/errors"
	"github.com/interuss/dss/pkg/logging"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/interuss/stacktrace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/status"
)

var (
	address         = flag.String("addr", ":8080", "Local address that the gateway binds to and listens on for incoming connections")
	traceRequests   = flag.Bool("trace-requests", false, "Logs HTTP request/response pairs to stderr if true")
	coreService     = flag.String("core-service", "", "Endpoint for core service. Only to be set if run in proxy mode")
	profServiceName = flag.String("gcp_prof_service_name", "", "Service name for the Go profiler")
	enableSCD       = flag.Bool("enable_scd", false, "Enables the Strategic Conflict Detection API")
)

const (
	codeRetryable = stacktrace.ErrorCode(1)
)

// RunHTTPProxy starts the HTTP proxy for the DSS gRPC service on ctx, listening
// on address, proxying to endpoint.
func RunHTTPProxy(ctx context.Context, ctxCanceler func(), address, endpoint string) error {
	logger := logging.WithValuesFromContext(ctx, logging.Logger).With(
		zap.String("address", address), zap.String("endpoint", endpoint),
	)

	logger.Info("build", zap.Any("description", build.Describe()))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	runtime.HTTPError = myHTTPError

	// Register gRPC server endpoint
	// Note: Make sure the gRPC server is running properly and accessible
	grpcMux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			OrigName:     true,
			EmitDefaults: true, // Include empty JSON arrays.
			Indent:       "  ",
		}),
	)

	opts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithBlock(),
		//lint:ignore SA1019 This is required as an argument to a generated function.
		grpc.WithTimeout(10 * time.Second),
	}

	logger.Info("Registering RID v1 service")
	if err := ridpbv1.RegisterDiscoveryAndSynchronizationServiceHandlerFromEndpoint(ctx, grpcMux, endpoint, opts); err != nil {
		// TODO: More robustly detect failure to create RID server is due to a problem that may be temporary
		if strings.Contains(err.Error(), "context deadline exceeded") {
			return stacktrace.PropagateWithCode(err, codeRetryable, "Failed to connect to core-service for remote ID v1")
		}
		return stacktrace.Propagate(err, "Error registering RID v1 service handler")
	}

	logger.Info("Registering RID v2 service")
	if err := ridpbv2.RegisterStandardRemoteIDAPIInterfacesServiceHandlerFromEndpoint(ctx, grpcMux, endpoint, opts); err != nil {
		if strings.Contains(err.Error(), "context deadline exceeded") {
			return stacktrace.PropagateWithCode(err, codeRetryable, "Failed to connect to core-service for remote ID v2")
		}
		return stacktrace.Propagate(err, "Error registering RID v2 service handler")
	}

	logger.Info("Registering aux service")
	if err := auxpb.RegisterDSSAuxServiceHandlerFromEndpoint(ctx, grpcMux, endpoint, opts); err != nil {
		// TODO: More robustly detect failure to create aux server is due to a problem that may be temporary
		if strings.Contains(err.Error(), "context deadline exceeded") {
			return stacktrace.PropagateWithCode(err, codeRetryable, "Failed to connect to core-service for aux")
		}
		return stacktrace.Propagate(err, "Error registering aux service handler")
	}

	logger.Info("Registering SCD service")
	if *enableSCD {
		if err := scdpb.RegisterUTMAPIUSSDSSAndUSSUSSServiceHandlerFromEndpoint(ctx, grpcMux, endpoint, opts); err != nil {
			// TODO: More robustly detect failure to create SCD server is due to a problem that may be temporary
			if strings.Contains(err.Error(), "context deadline exceeded") {
				return stacktrace.PropagateWithCode(err, codeRetryable, "Failed to connect to core-service for strategic conflict detection")
			}
			return stacktrace.Propagate(err, "Error registering SCD service handler")
		}
		logger.Info("config", zap.Any("scd", "enabled"))
	} else {
		logger.Info("config", zap.Any("scd", "disabled"))
	}

	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthy" {
			if _, err := w.Write([]byte("ok")); err != nil {
				logger.Error("Error writing to /healthy")
			}
		} else {
			grpcMux.ServeHTTP(w, r)
		}
	})

	if *traceRequests {
		handler = logging.HTTPMiddleware(logger, handler)
	}

	signals := make(chan os.Signal)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	server := &http.Server{
		Addr:    address,
		Handler: handler,
	}

	go func() {
		defer func() {
			if err := server.Shutdown(context.Background()); err != nil {
				logger.Warn("failed to shut down http server", zap.Error(err))
			}
		}()

		for {
			select {
			case <-ctx.Done():
				logger.Info("stopping server due to context having been canceled")
				return
			case s := <-signals:
				logger.Info("received OS signal", zap.Stringer("signal", s))
				ctxCanceler()
			}
		}
	}()

	// Indicate ready for container health checks
	readyFile, err := os.Create("service.ready")
	if err != nil {
		return stacktrace.Propagate(err, "Error touching file to indicate service ready")
	}
	readyFile.Close()

	// Start HTTP server (and proxy calls to gRPC server endpoint)
	logger.Info("Starting HTTP server")
	return server.ListenAndServe()
}

func myCodeToHTTPStatus(code codes.Code) int {
	switch code {
	case codes.OK:
		return http.StatusOK
	case codes.Canceled:
		return http.StatusRequestTimeout
	case codes.Unknown:
		return http.StatusInternalServerError
	case codes.InvalidArgument:
		return http.StatusBadRequest
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout
	case codes.NotFound:
		return http.StatusNotFound
	case codes.AlreadyExists:
		return http.StatusConflict
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	case codes.FailedPrecondition:
		// Note, this deliberately doesn't translate to the similarly named '412 Precondition Failed' HTTP response status.
		return http.StatusBadRequest
	case codes.Aborted:
		return http.StatusConflict
	case codes.OutOfRange:
		return http.StatusBadRequest
	case codes.Unimplemented:
		return http.StatusNotImplemented
	case codes.Internal:
		return http.StatusInternalServerError
	case codes.Unavailable:
		return http.StatusServiceUnavailable
	case codes.DataLoss:
		return http.StatusInternalServerError
	case codes.Code(uint16(errors.AreaTooLarge)):
		return http.StatusRequestEntityTooLarge
	case codes.Code(uint16(errors.MissingOVNs)):
		return http.StatusConflict
	}

	grpclog.Warningf("Unknown gRPC error code: %v", code)
	return http.StatusInternalServerError
}

// this method was copied directly from github.com/grpc-ecosystem/grpc-gateway/runtime/errors
// we initially only needed to add 1 extra Code to handle but since they didn't
// export HTTPStatusFromCode we had to copy the whole thing.  Since then, we have added
// custom error handling to return additional content for certain errors.  This handler
// is invoked whenever the call to the Core Service results in an error (thus returning
// a Status err).  Because an error has occurred, the normal response body is not returned.
func myHTTPError(ctx context.Context, mux *runtime.ServeMux, marshaler runtime.Marshaler, w http.ResponseWriter, _ *http.Request, err error) {
	errID := errors.MakeErrID()
	fallback := fmt.Sprintf(
		`{"error": "Internal server error (fallback) %s", "message": "Internal server error (fallback) %s", "error_id": "%s", "code": %d}`,
		errID, errID, errID, codes.Internal)

	s, ok := status.FromError(err)
	if !ok {
		s = status.New(codes.Unknown, err.Error())
	}

	w.Header().Del("Trailer")

	contentType := marshaler.ContentType()
	// Check marshaler on run time in order to keep backwards compatibility
	// An interface param needs to be added to the ContentType() function on
	// the Marshal interface to be able to remove this check
	if httpBodyMarshaler, ok := marshaler.(*runtime.HTTPBodyMarshaler); ok {
		pb := s.Proto()
		contentType = httpBodyMarshaler.ContentTypeFromMessage(pb)
	}
	w.Header().Set("Content-Type", contentType)

	// Marshal error content into buf
	var buf []byte
	var marshalingErr error
	handled := false
	if s.Code() == codes.Code(uint16(errors.MissingOVNs)) {
		// Handle special return schema for missing OVNs
		if len(s.Details()) < 1 {
			marshalingErr = stacktrace.NewError("Missing Details from Status")
		} else {
			body, ok := s.Details()[0].(*scdpb.AirspaceConflictResponse)
			if ok {
				buf, marshalingErr = marshaler.Marshal(body)
				grpclog.Errorf("Error %s was an AirspaceConflictResponse from the Core Service", errID)
			} else {
				marshalingErr = stacktrace.NewError("Unable to cast s.Details()[0] from %s to *scdpb.AirspaceConflictResponse", reflect.TypeOf(s.Details()[0]))
			}
		}
		handled = true
	} else if len(s.Details()) == 1 {
		// Handle explicit error responses
		result, ok := s.Details()[0].(*auxpb.StandardErrorResponse)
		if ok {
			buf, marshalingErr = marshaler.Marshal(result)
			grpclog.Errorf("Error %s was a StandardErrorResponse from the Core Service", errID)
			handled = true
		}
	}
	if !handled {
		// Default error-handling schema
		body := &auxpb.StandardErrorResponse{
			Error:   s.Message(),
			Message: s.Message(),
			Code:    int32(s.Code()),
			ErrorId: errID,
		}
		grpclog.Errorf("Error %s during a request did not include Details in Status; constructed code %d, message `%s`", errID, body.Code, body.Message)

		buf, marshalingErr = marshaler.Marshal(body)
		if marshalingErr != nil {
			grpclog.Errorf("Error %s: Failed to marshal default error response %q: %v", errID, body, marshalingErr)
		}
	} else if marshalingErr != nil {
		grpclog.Errorf("Error %s: Failed to marshal response: %v", errID, marshalingErr)
	}

	if marshalingErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		if _, err := io.WriteString(w, fallback); err != nil {
			grpclog.Errorf("Error %s: Failed to write response: %v", errID, err)
		}
		return
	}

	md, ok := runtime.ServerMetadataFromContext(ctx)
	if !ok {
		grpclog.Errorf("Error %s: Failed to extract ServerMetadata from context", errID)
	}

	handleForwardResponseServerMetadata(w, mux, md)
	handleForwardResponseTrailerHeader(w, md)
	st := myCodeToHTTPStatus(s.Code())
	w.WriteHeader(st)
	if _, err := w.Write(buf); err != nil {
		grpclog.Errorf("Error %s: Failed to write response: %v", errID, err)
	}

	handleForwardResponseTrailer(w, md)
}

func handleForwardResponseServerMetadata(w http.ResponseWriter, mux *runtime.ServeMux, md runtime.ServerMetadata) {
	for k, vs := range md.HeaderMD {
		if h, ok := runtime.DefaultHeaderMatcher(k); ok {
			for _, v := range vs {
				w.Header().Add(h, v)
			}
		}
	}
}

func handleForwardResponseTrailerHeader(w http.ResponseWriter, md runtime.ServerMetadata) {
	for k := range md.TrailerMD {
		tKey := textproto.CanonicalMIMEHeaderKey(fmt.Sprintf("%s%s", runtime.MetadataTrailerPrefix, k))
		w.Header().Add("Trailer", tKey)
	}
}

func handleForwardResponseTrailer(w http.ResponseWriter, md runtime.ServerMetadata) {
	for k, vs := range md.TrailerMD {
		tKey := fmt.Sprintf("%s%s", runtime.MetadataTrailerPrefix, k)
		for _, v := range vs {
			w.Header().Add(tKey, v)
		}
	}
}

func main() {
	flag.Parse()
	var (
		ctx, cancel = context.WithCancel(context.Background())
		logger      = logging.WithValuesFromContext(ctx, logging.Logger)
	)
	defer cancel()

	if *profServiceName != "" {
		err := profiler.Start(
			profiler.Config{
				Service: *profServiceName})
		if err != nil {
			logger.Panic("Failed to start the profiler ", zap.Error(err))
		}
	}

	backoffs := []time.Duration{
		5 * time.Second, 15 * time.Second, 1 * time.Minute, 1 * time.Minute,
		1 * time.Minute, 5 * time.Minute}
	backoff := 0
	for {
		if err := RunHTTPProxy(ctx, cancel, *address, *coreService); err != nil {
			if stacktrace.GetCode(err) == codeRetryable {
				logger.Info(fmt.Sprintf("Prerequisites not yet satisfied; waiting %ds to retry...", backoffs[backoff]/1000000000), zap.Error(err))
				time.Sleep(backoffs[backoff])
				if backoff < len(backoffs)-1 {
					backoff++
				}
				continue
			}
			rootCause := stacktrace.RootCause(err)
			if rootCause == nil || rootCause == context.Canceled || rootCause == http.ErrServerClosed {
				logger.Info("Shutting down gracefully")
				break
			}
			logger.Panic("Failed to execute service", zap.Error(err))
		}
		break
	}
}
