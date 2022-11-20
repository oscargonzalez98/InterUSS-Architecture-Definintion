package scd

import (
	"context"

	"github.com/golang/geo/s2"
	"github.com/interuss/dss/pkg/api/v1/scdpb"
	"github.com/interuss/dss/pkg/auth"
	dsserr "github.com/interuss/dss/pkg/errors"
	"github.com/interuss/dss/pkg/geo"
	dssmodels "github.com/interuss/dss/pkg/models"
	scdmodels "github.com/interuss/dss/pkg/scd/models"
	"github.com/interuss/dss/pkg/scd/repos"
	"github.com/interuss/stacktrace"
	"github.com/jonboulle/clockwork"
)

var (
	DefaultClock = clockwork.NewRealClock()
)

func (a *Server) CreateSubscription(ctx context.Context, req *scdpb.CreateSubscriptionRequest) (*scdpb.PutSubscriptionResponse, error) {
	return a.PutSubscription(ctx, req.GetSubscriptionid(), "", req.GetParams())
}

func (a *Server) UpdateSubscription(ctx context.Context, req *scdpb.UpdateSubscriptionRequest) (*scdpb.PutSubscriptionResponse, error) {
	version := req.GetVersion()
	return a.PutSubscription(ctx, req.GetSubscriptionid(), version, req.GetParams())
}

// PutSubscription creates a single subscription.
func (a *Server) PutSubscription(ctx context.Context, subscriptionid string, version string, params *scdpb.PutSubscriptionParameters) (*scdpb.PutSubscriptionResponse, error) {
	// Retrieve Subscription ID
	id, err := dssmodels.IDFromString(subscriptionid)

	if err != nil {
		return nil, stacktrace.NewErrorWithCode(dsserr.BadRequest, "Invalid ID format: `%s`", subscriptionid)
	}

	// Retrieve ID of client making call
	manager, ok := auth.ManagerFromContext(ctx)
	if !ok {
		return nil, stacktrace.NewErrorWithCode(dsserr.PermissionDenied, "Missing owner from context")
	}

	if !a.EnableHTTP {
		err = scdmodels.ValidateUSSBaseURL(params.UssBaseUrl)
		if err != nil {
			return nil, stacktrace.PropagateWithCode(err, dsserr.BadRequest, "Failed to validate base URL")
		}
	}

	// Parse extents
	extents, err := dssmodels.Volume4DFromSCDProto(params.GetExtents())
	if err != nil {
		return nil, stacktrace.PropagateWithCode(err, dsserr.BadRequest, "Unable to parse extents")
	}

	// Construct requested Subscription model
	cells, err := extents.CalculateSpatialCovering()
	switch err {
	case nil, geo.ErrMissingSpatialVolume, geo.ErrMissingFootprint:
		// We may be able to fill these values from a previous Subscription or via defaults.
	default:
		return nil, stacktrace.PropagateWithCode(err, dsserr.BadRequest, "Invalid area")
	}

	subreq := &scdmodels.Subscription{
		ID:      id,
		Manager: manager,
		Version: scdmodels.OVN(version),

		StartTime:  extents.StartTime,
		EndTime:    extents.EndTime,
		AltitudeLo: extents.SpatialVolume.AltitudeLo,
		AltitudeHi: extents.SpatialVolume.AltitudeHi,
		Cells:      cells,

		USSBaseURL:                  params.UssBaseUrl,
		NotifyForOperationalIntents: params.NotifyForOperationalIntents,
		NotifyForConstraints:        params.NotifyForConstraints,
	}

	// Validate requested Subscription
	if !subreq.NotifyForOperationalIntents && !subreq.NotifyForConstraints {
		return nil, stacktrace.NewErrorWithCode(dsserr.BadRequest, "No notification triggers requested for Subscription")
	}

	// TODO: Check scopes to verify requested information (op intents or constraints) may be requested

	var result *scdpb.PutSubscriptionResponse
	action := func(ctx context.Context, r repos.Repository) (err error) {
		// Check existing Subscription (if any)
		old, err := r.GetSubscription(ctx, subreq.ID)
		if err != nil {
			return stacktrace.Propagate(err, "Could not get Subscription from repo")
		}

		// Validate and perhaps correct StartTime and EndTime.
		if err := subreq.AdjustTimeRange(DefaultClock.Now(), old); err != nil {
			return stacktrace.Propagate(err, "Error adjusting time range of Subscription")
		}

		var dependentOpIds []dssmodels.ID

		if old == nil {
			// There is no previous Subscription (this is a creation attempt)
			if subreq.Version.String() != "" {
				// The user wants to update an existing Subscription, but one wasn't found.
				return stacktrace.NewErrorWithCode(dsserr.NotFound, "Subscription %s not found", subreq.ID.String())
			}
		} else {
			// There is a previous Subscription (this is an update attempt)
			switch {
			case subreq.Version.String() == "":
				// The user wants to create a new Subscription but it already exists.
				return stacktrace.NewErrorWithCode(dsserr.AlreadyExists, "Subscription %s already exists", subreq.ID.String())
			case subreq.Version.String() != old.Version.String():
				// The user wants to update a Subscription but the version doesn't match.
				return stacktrace.Propagate(
					stacktrace.NewErrorWithCode(dsserr.VersionMismatch, "Subscription version %s is not current", subreq.Version),
					"Current version is %s but client specified version %s", old.Version, subreq.Version)
			case old.Manager != subreq.Manager:
				return stacktrace.Propagate(
					stacktrace.NewErrorWithCode(dsserr.PermissionDenied, "Subscription is owned by different client"),
					"Subscription owned by %s, but %s attempted to modify", old.Manager, subreq.Manager)
			}

			subreq.NotificationIndex = old.NotificationIndex

			// Validate Subscription against DependentOperations
			dependentOpIds, err = r.GetDependentOperationalIntents(ctx, subreq.ID)
			if err != nil {
				return stacktrace.Propagate(err, "Could not find dependent Operation Ids")
			}

			operations, err := GetOperations(ctx, r, dependentOpIds)
			if err != nil {
				return stacktrace.Propagate(err, "Could not get all dependent Operations")
			}
			if err := subreq.ValidateDependentOps(operations); err != nil {
				// The provided subscription does not cover all its dependent operations
				return err
			}
		}

		// Store Subscription model
		sub, err := r.UpsertSubscription(ctx, subreq)
		if err != nil {
			return stacktrace.Propagate(err, "Could not upsert Subscription into repo")
		}
		if sub == nil {
			return stacktrace.NewError("UpsertSubscription returned no Subscription for ID: %s", id)
		}

		// Find relevant Operations
		var relevantOperations []*scdmodels.OperationalIntent
		if len(sub.Cells) > 0 {
			ops, err := r.SearchOperationalIntents(ctx, &dssmodels.Volume4D{
				StartTime: sub.StartTime,
				EndTime:   sub.EndTime,
				SpatialVolume: &dssmodels.Volume3D{
					AltitudeLo: sub.AltitudeLo,
					AltitudeHi: sub.AltitudeHi,
					Footprint: dssmodels.GeometryFunc(func() (s2.CellUnion, error) {
						return sub.Cells, nil
					}),
				},
			})
			if err != nil {
				return stacktrace.Propagate(err, "Could not search Operations in repo")
			}
			relevantOperations = ops
		}

		// Convert Subscription to proto
		p, err := sub.ToProto(dependentOpIds)
		if err != nil {
			return stacktrace.Propagate(err, "Could not convert Subscription to proto")
		}
		result = &scdpb.PutSubscriptionResponse{
			Subscription: p,
		}

		if sub.NotifyForOperationalIntents {
			// Attach Operations to response
			for _, op := range relevantOperations {
				if op.Manager != manager {
					op.OVN = scdmodels.OVN(scdmodels.NoOvnPhrase)
				}
				pop, _ := op.ToProto()
				result.OperationalIntentReferences = append(result.OperationalIntentReferences, pop)
			}
		}

		if sub.NotifyForConstraints {
			// Query relevant Constraints
			constraints, err := r.SearchConstraints(ctx, extents)
			if err != nil {
				return stacktrace.Propagate(err, "Could not search Constraints in repo")
			}

			// Attach Constraints to response
			for _, constraint := range constraints {
				p, err := constraint.ToProto()
				if err != nil {
					return stacktrace.Propagate(err, "Could not convert Constraint to proto")
				}
				if constraint.Manager != manager {
					p.Ovn = scdmodels.NoOvnPhrase
				}
				result.ConstraintReferences = append(result.ConstraintReferences, p)
			}
		}

		return nil
	}

	err = a.Store.Transact(ctx, action)
	if err != nil {
		return nil, err // No need to Propagate this error as this is not a useful stacktrace line
	}

	// Return response to client
	return result, nil
}

// GetSubscription returns a single subscription for the given ID.
func (a *Server) GetSubscription(ctx context.Context, req *scdpb.GetSubscriptionRequest) (*scdpb.GetSubscriptionResponse, error) {
	// Retrieve Subscription ID
	id, err := dssmodels.IDFromString(req.GetSubscriptionid())
	if err != nil {
		return nil, stacktrace.NewErrorWithCode(dsserr.BadRequest, "Invalid ID format: `%s`", req.GetSubscriptionid())
	}

	// Retrieve ID of client making call
	manager, ok := auth.ManagerFromContext(ctx)
	if !ok {
		return nil, stacktrace.NewErrorWithCode(dsserr.PermissionDenied, "Missing owner from context")
	}

	var response *scdpb.GetSubscriptionResponse
	action := func(ctx context.Context, r repos.Repository) (err error) {
		// Get Subscription from Store
		sub, err := r.GetSubscription(ctx, id)
		if err != nil {
			return stacktrace.Propagate(err, "Could not get Subscription from repo")
		}
		if sub == nil {
			return stacktrace.NewErrorWithCode(dsserr.NotFound, "Subscription %s not found", id.String())
		}

		// Check if the client is authorized to view this Subscription
		if manager != sub.Manager {
			return stacktrace.Propagate(
				stacktrace.NewErrorWithCode(dsserr.PermissionDenied, "Subscription is owned by different client"),
				"Subscription owned by %s, but %s attempted to view", sub.Manager, manager)
		}

		// Get dependent Operations
		dependentOps, err := r.GetDependentOperationalIntents(ctx, id)
		if err != nil {
			return stacktrace.Propagate(err, "Could not find dependent Operations")
		}

		// Convert Subscription to proto
		p, err := sub.ToProto(dependentOps)
		if err != nil {
			return stacktrace.Propagate(err, "Unable to convert Subscription to proto")
		}

		// Return response to client
		response = &scdpb.GetSubscriptionResponse{
			Subscription: p,
		}

		return nil
	}

	err = a.Store.Transact(ctx, action)
	if err != nil {
		return nil, err // No need to Propagate this error as this is not a useful stacktrace line
	}

	return response, nil
}

// QuerySubscriptions queries existing subscriptions in the given bounds.
func (a *Server) QuerySubscriptions(ctx context.Context, req *scdpb.QuerySubscriptionsRequest) (*scdpb.QuerySubscriptionsResponse, error) {
	// Retrieve the area of interest parameter
	aoi := req.GetParams().AreaOfInterest
	if aoi == nil {
		return nil, stacktrace.NewErrorWithCode(dsserr.BadRequest, "Missing area_of_interest")
	}

	// Parse area of interest to common Volume4D
	vol4, err := dssmodels.Volume4DFromSCDProto(aoi)
	if err != nil {
		return nil, stacktrace.PropagateWithCode(err, dsserr.BadRequest, "Failed to convert to internal geometry model")
	}

	// Retrieve ID of client making call
	manager, ok := auth.ManagerFromContext(ctx)
	if !ok {
		return nil, stacktrace.NewErrorWithCode(dsserr.PermissionDenied, "Missing owner from context")
	}

	var response *scdpb.QuerySubscriptionsResponse
	action := func(ctx context.Context, r repos.Repository) (err error) {
		// Perform search query on Store
		subs, err := r.SearchSubscriptions(ctx, vol4)
		if err != nil {
			return stacktrace.Propagate(err, "Error searching Subscriptions in repo")
		}

		// Return response to client
		response = &scdpb.QuerySubscriptionsResponse{}
		for _, sub := range subs {
			if sub.Manager == manager {
				// Get dependent Operations
				dependentOps, err := r.GetDependentOperationalIntents(ctx, sub.ID)
				if err != nil {
					return stacktrace.Propagate(err, "Could not find dependent Operations")
				}

				p, err := sub.ToProto(dependentOps)
				if err != nil {
					return stacktrace.Propagate(err, "Error converting Subscription model to proto")
				}
				response.Subscriptions = append(response.Subscriptions, p)
			}
		}

		return nil
	}

	err = a.Store.Transact(ctx, action)
	if err != nil {
		return nil, err // No need to Propagate this error as this is not a useful stacktrace line
	}

	return response, nil
}

// DeleteSubscription deletes a single subscription for a given ID.
func (a *Server) DeleteSubscription(ctx context.Context, req *scdpb.DeleteSubscriptionRequest) (*scdpb.DeleteSubscriptionResponse, error) {
	// Retrieve Subscription ID
	id, err := dssmodels.IDFromString(req.GetSubscriptionid())
	if err != nil {
		return nil, stacktrace.NewErrorWithCode(dsserr.BadRequest, "Invalid ID format")
	}

	// Retrieve ID of client making call
	manager, ok := auth.ManagerFromContext(ctx)
	if !ok {
		return nil, stacktrace.NewErrorWithCode(dsserr.PermissionDenied, "Missing owner from context")
	}

	var response *scdpb.DeleteSubscriptionResponse
	action := func(ctx context.Context, r repos.Repository) (err error) {
		// Check to make sure it's ok to delete this Subscription
		old, err := r.GetSubscription(ctx, id)
		switch {
		case err != nil:
			return stacktrace.Propagate(err, "Could not get Subscription from repo")
		case old == nil: // Return a 404 here.
			return stacktrace.NewErrorWithCode(dsserr.NotFound, "Subscription %s not found", id.String())
		case old.Manager != manager:
			return stacktrace.Propagate(
				stacktrace.NewErrorWithCode(dsserr.PermissionDenied, "Subscription is owned by different client"),
				"Subscription owned by %s, but %s attempted to delete", old.Manager, manager)
		}

		// Get dependent Operations
		dependentOps, err := r.GetDependentOperationalIntents(ctx, id)
		if err != nil {
			return stacktrace.Propagate(err, "Could not find dependent Operations")
		}
		if len(dependentOps) > 0 {
			return stacktrace.Propagate(
				stacktrace.NewErrorWithCode(dsserr.BadRequest, "Subscriptions with dependent Operations may not be removed"),
				"Subscription had %d dependent Operations", len(dependentOps))
		}

		// Delete Subscription in repo
		err = r.DeleteSubscription(ctx, id)
		if err != nil {
			return stacktrace.Propagate(err, "Could not delete Subscription from repo")
		}

		// Convert deleted Subscription to proto
		p, err := old.ToProto(dependentOps)
		if err != nil {
			return stacktrace.Propagate(err, "Error converting Subscription model to proto")
		}

		// Create response for client
		response = &scdpb.DeleteSubscriptionResponse{
			Subscription: p,
		}

		return nil
	}

	err = a.Store.Transact(ctx, action)
	if err != nil {
		return nil, err // No need to Propagate this error as this is not a useful stacktrace line
	}

	return response, nil
}

// GetOperations gets operations by given ids
func GetOperations(ctx context.Context, r repos.Repository, opIDs []dssmodels.ID) ([]*scdmodels.OperationalIntent, error) {
	var res []*scdmodels.OperationalIntent
	for _, opID := range opIDs {
		operation, err := r.GetOperationalIntent(ctx, opID)
		if err != nil {
			return nil, stacktrace.Propagate(err, "Could not retrieve dependent Operation %s", opID)
		}
		res = append(res, operation)
	}
	return res, nil
}
