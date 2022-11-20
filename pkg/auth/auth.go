package auth

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	dsserr "github.com/interuss/dss/pkg/errors"
	"github.com/interuss/dss/pkg/logging"
	"github.com/interuss/dss/pkg/models"

	"github.com/golang-jwt/jwt"
	"github.com/interuss/stacktrace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"gopkg.in/square/go-jose.v2"
)

var (
	// ContextKeyOwner is the key to an owner value.
	ContextKeyOwner ContextKey = "owner"
)

// ContextKey models auth-specific keys in a context.
type ContextKey string

type missingScopesError struct {
	s []string
}

func (m *missingScopesError) Error() string {
	return strings.Join(m.s, ", ")
}

// ContextWithOwner adds "owner" to "ctx".
func ContextWithOwner(ctx context.Context, owner models.Owner) context.Context {
	return context.WithValue(ctx, ContextKeyOwner, owner)
}

// OwnerFromContext returns the value for owner from "ctx" and a boolean
// indicating whether a valid value was present or not.
func OwnerFromContext(ctx context.Context) (models.Owner, bool) {
	owner, ok := ctx.Value(ContextKeyOwner).(models.Owner)
	return owner, ok
}

// ManagerFromContext returns the value for manager from "ctx" and a boolean
// indicating whether a valid value was present or not.
func ManagerFromContext(ctx context.Context) (models.Manager, bool) {
	owner, ok := OwnerFromContext(ctx)
	return models.Manager(owner), ok
}

// KeyResolver abstracts resolving keys.
type KeyResolver interface {
	// ResolveKey returns a public or private key, most commonly an rsa.PublicKey.
	ResolveKeys(context.Context) ([]interface{}, error)
}

type fromMemoryKeyResolver struct {
	Keys []interface{}
}

// ResolveKeys returns the set of keys provided to the fromMemoryKeyResolver.
func (r *fromMemoryKeyResolver) ResolveKeys(context.Context) ([]interface{}, error) {
	return r.Keys, nil
}

// FromFileKeyResolver resolves keys from 'KeyFile'.
type FromFileKeyResolver struct {
	KeyFiles []string
	keys     []interface{}
}

// ResolveKeys resolves an RSA public key from file for verifying JWTs.
func (r *FromFileKeyResolver) ResolveKeys(context.Context) ([]interface{}, error) {
	if r.keys != nil {
		return r.keys, nil
	}

	for _, f := range r.KeyFiles {
		bytes, err := ioutil.ReadFile(f)
		if err != nil {
			return nil, stacktrace.Propagate(err, "Error reading key file")
		}
		pub, _ := pem.Decode(bytes)
		if pub == nil {
			return nil, stacktrace.NewError("Failed to decode key file")
		}
		parsedKey, err := x509.ParsePKIXPublicKey(pub.Bytes)
		if err != nil {
			return nil, stacktrace.Propagate(err, "Error parsing key as x509 public key")
		}
		key, ok := parsedKey.(*rsa.PublicKey)
		if !ok {
			return nil, stacktrace.NewError("Could not create RSA public key from %s", f)
		}
		r.keys = append(r.keys, key)
	}
	return r.keys, nil
}

// JWKSResolver resolves the key(s) with ID 'KeyID' from 'Endpoint' serving
// JWK sets.
type JWKSResolver struct {
	Endpoint *url.URL
	// If empty, will use all the keys provided by the jwks Endpoint.
	KeyIDs []string
}

// ResolveKeys resolves an RSA public key from file for verifying JWTs.
func (r *JWKSResolver) ResolveKeys(ctx context.Context) ([]interface{}, error) {
	req := http.Request{
		Method: http.MethodGet,
		URL:    r.Endpoint,
	}

	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, stacktrace.Propagate(err, fmt.Sprintf("Error retrieving JWKS at %s", req.URL))
	}
	defer resp.Body.Close()

	jwks := jose.JSONWebKeySet{}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, stacktrace.Propagate(err, "Error decoding JWKS")
	}

	var keys []interface{}
	var webKeys []jose.JSONWebKey
	if len(r.KeyIDs) == 0 {
		webKeys = jwks.Keys
	}
	for _, kid := range r.KeyIDs {
		// jwks.Key returns a slice of keys.
		jkeys := jwks.Key(kid)
		if len(jkeys) == 0 {
			return nil, stacktrace.NewError("Failed to resolve key(s) for ID: %s", kid)
		}
		webKeys = append(webKeys, jkeys...)
	}
	for _, w := range webKeys {
		keys = append(keys, w.Key)
	}
	return keys, nil
}

// KeyClaimedScopesValidator validates a set of scopes claimed by an incoming
// JWT.
type KeyClaimedScopesValidator interface {
	// ValidateKeyClaimedScopes returns an error if 'scopes' are not sufficient
	// to authorize an operation, nil otherwise.
	ValidateKeyClaimedScopes(ctx context.Context, scopes ScopeSet) error

	// Expectation returns a string indicating the scopes expected to validate
	// successfully.
	Expectation() string
}

type allScopesRequiredValidator struct {
	scopes []Scope
}

func (v *allScopesRequiredValidator) ValidateKeyClaimedScopes(ctx context.Context, scopes ScopeSet) error {
	var (
		missing []string
	)

	for _, scope := range v.scopes {
		if _, present := scopes[scope]; !present {
			missing = append(missing, scope.String())
		}
	}

	if len(missing) > 0 {
		return &missingScopesError{
			s: missing,
		}
	}

	return nil
}

func scopeSetToString(scopes ScopeSet, separator string) string {
	var stringScopes []string
	for scope := range scopes {
		stringScopes = append(stringScopes, scope.String())
	}
	return strings.Join(stringScopes, separator)
}

func scopesToString(scopes []Scope, separator string) string {
	var stringScopes []string
	for _, scope := range scopes {
		stringScopes = append(stringScopes, scope.String())
	}
	return strings.Join(stringScopes, separator)
}

func (v *allScopesRequiredValidator) Expectation() string {
	return scopesToString(v.scopes, " and ")
}

// RequireAllScopes returns a KeyClaimedScopesValidator instance ensuring that
// every element in scopes is claimed by an incoming set of scopes.
func RequireAllScopes(scopes ...Scope) KeyClaimedScopesValidator {
	return &allScopesRequiredValidator{
		scopes: scopes,
	}
}

type anyScopesRequiredValidator struct {
	scopes []Scope
}

func (v *anyScopesRequiredValidator) ValidateKeyClaimedScopes(ctx context.Context, scopes ScopeSet) error {
	var (
		missing []string
	)

	for _, scope := range v.scopes {
		if _, present := scopes[scope]; present {
			return nil
		}
		missing = append(missing, scope.String())
	}

	return &missingScopesError{
		s: missing,
	}
}

func (v *anyScopesRequiredValidator) Expectation() string {
	return scopesToString(v.scopes, " or ")
}

// RequireAnyScope returns a KeyClaimedScopesValidator instance ensuring that
// at least one element in scopes is claimed by an incoming set of scopes.
func RequireAnyScope(scopes ...Scope) KeyClaimedScopesValidator {
	return &anyScopesRequiredValidator{
		scopes: scopes,
	}
}

// Authorizer authorizes incoming requests.
type Authorizer struct {
	logger            *zap.Logger
	keys              []interface{}
	keyGuard          sync.RWMutex
	scopesValidators  map[Operation]KeyClaimedScopesValidator
	acceptedAudiences map[string]bool
}

// Configuration bundles up creation-time parameters for an Authorizer instance.
type Configuration struct {
	KeyResolver       KeyResolver                             // Used to initialize and periodically refresh keys.
	KeyRefreshTimeout time.Duration                           // Keys are refreshed on this cadence.
	ScopesValidators  map[Operation]KeyClaimedScopesValidator // ScopesValidators are used to enforce authorization for operations.
	AcceptedAudiences []string                                // AcceptedAudiences enforces the aud keyClaim on the jwt. An empty string allows no aud keyClaim.
}

// NewRSAAuthorizer returns an Authorizer instance using values from configuration.
func NewRSAAuthorizer(ctx context.Context, configuration Configuration) (*Authorizer, error) {
	logger := logging.WithValuesFromContext(ctx, logging.Logger)

	keys, err := configuration.KeyResolver.ResolveKeys(ctx)
	if err != nil {
		return nil, stacktrace.Propagate(err, "Unable to resolve keys")
	}

	auds := make(map[string]bool)
	for _, s := range configuration.AcceptedAudiences {
		auds[s] = true
	}

	authorizer := &Authorizer{
		scopesValidators:  configuration.ScopesValidators,
		acceptedAudiences: auds,
		logger:            logger,
		keys:              keys,
	}

	go func() {
		ticker := time.NewTicker(configuration.KeyRefreshTimeout)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				keys, err := configuration.KeyResolver.ResolveKeys(ctx)
				if err != nil {
					logger.Panic("failed to refresh key", zap.Error(err))
				}

				authorizer.setKeys(keys)
			case <-ctx.Done():
				logger.Warn("finalizing key refresh worker", zap.Error(ctx.Err()))
				return
			}
		}
	}()

	return authorizer, nil
}

func (a *Authorizer) setKeys(keys []interface{}) {
	a.keyGuard.Lock()
	a.keys = keys
	a.keyGuard.Unlock()
}

// AuthInterceptor intercepts incoming gRPC requests and extracts and verifies
// accompanying bearer tokens.
func (a *Authorizer) AuthInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {

	tknStr, ok := getToken(ctx)
	if !ok {
		return nil, stacktrace.NewErrorWithCode(dsserr.Unauthenticated, "Missing access token")
	}

	a.keyGuard.RLock()
	keys := a.keys
	a.keyGuard.RUnlock()
	validated := false
	var err error
	var keyClaims claims

	for _, key := range keys {
		keyClaims = claims{}
		key := key
		_, err = jwt.ParseWithClaims(tknStr, &keyClaims, func(token *jwt.Token) (interface{}, error) {
			return key, nil
		})
		if err == nil {
			validated = true
			break
		}
	}
	if !validated {
		return nil, stacktrace.PropagateWithCode(err, dsserr.Unauthenticated, "Access token validation failed")
	}

	if !a.acceptedAudiences[keyClaims.Audience] {
		return nil, stacktrace.NewErrorWithCode(dsserr.Unauthenticated,
			"Invalid access token audience: %v", keyClaims.Audience)
	}

	expectation, err := a.validateKeyClaimedScopes(ctx, info, keyClaims.Scopes)
	if err != nil {
		return nil, stacktrace.NewErrorWithCode(dsserr.PermissionDenied, "Access token missing scopes; found %v while expecting %v", scopeSetToString(keyClaims.Scopes, ", "), expectation)
	}

	return handler(ContextWithOwner(ctx, models.Owner(keyClaims.Subject)), req)
}

// Matches keyClaimedScopes against the required scopes and returns nil, nil if
// keyClaimedScopes satisifies the authorizer, otherwise returns the expectation
// and the error.
func (a *Authorizer) validateKeyClaimedScopes(ctx context.Context, info *grpc.UnaryServerInfo, keyClaimedScopes ScopeSet) (string, error) {
	if validator, known := a.scopesValidators[Operation(info.FullMethod)]; known {
		err := validator.ValidateKeyClaimedScopes(ctx, keyClaimedScopes)
		expectation := ""
		if err != nil {
			expectation = validator.Expectation()
		}
		return expectation, err
	}

	return "", nil
}

func getToken(ctx context.Context) (string, bool) {
	headers, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", false
	}
	authHeader := headers.Get("authorization")
	if len(authHeader) == 0 {
		return "", false
	}

	// Remove Bearer before returning.
	return strings.TrimPrefix(authHeader[0], "Bearer "), true
}
