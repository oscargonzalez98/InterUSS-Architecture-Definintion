package cockroach

import (
	"context"
	"fmt"
	"time"

	dsserr "github.com/interuss/dss/pkg/errors"
	"github.com/interuss/dss/pkg/geo"
	dssmodels "github.com/interuss/dss/pkg/models"
	ridmodels "github.com/interuss/dss/pkg/rid/models"
	"github.com/jackc/pgtype"
	"github.com/jonboulle/clockwork"

	"github.com/golang/geo/s2"
	dssql "github.com/interuss/dss/pkg/sql"
	"github.com/interuss/stacktrace"
	"go.uber.org/zap"
)

const (
	subscriptionFieldsV3       = "id, owner, url, notification_index, cells, starts_at, ends_at, updated_at"
	updateSubscriptionFieldsV3 = "id, url, notification_index, cells, starts_at, ends_at, updated_at"
)

// subscriptions is an implementation of the SubscriptionRepo for CRDB.
type subscriptionRepoV3 struct {
	dssql.Queryable

	clock  clockwork.Clock
	logger *zap.Logger
}

// process a query that should return one or many subscriptions.
func (c *subscriptionRepoV3) process(ctx context.Context, query string, args ...interface{}) ([]*ridmodels.Subscription, error) {
	rows, err := c.Query(ctx, query, args...)
	if err != nil {
		return nil, stacktrace.Propagate(err, fmt.Sprintf("Error in query: %s", query))
	}
	defer rows.Close()

	var payload []*ridmodels.Subscription
	pgCids := pgtype.Int8Array{}

	for rows.Next() {
		s := new(ridmodels.Subscription)

		var updateTime time.Time

		err := rows.Scan(
			&s.ID,
			&s.Owner,
			&s.URL,
			&s.NotificationIndex,
			&pgCids,
			&s.StartTime,
			&s.EndTime,
			&updateTime,
		)
		if err != nil {
			return nil, stacktrace.Propagate(err, "Error scanning Subscription row")
		}
		var cids []int64
		if err := pgCids.AssignTo(&cids); err != nil {
			return nil, stacktrace.Propagate(err, "Error Converting jackc/pgtype to array")
		}
		s.SetCells(cids)
		s.Version = dssmodels.VersionFromTime(updateTime)
		payload = append(payload, s)
	}
	if err := rows.Err(); err != nil {
		return nil, stacktrace.Propagate(err, "Error in rows query result")
	}
	return payload, nil
}

// processOne processes a query that should return exactly a single subscription.
func (c *subscriptionRepoV3) processOne(ctx context.Context, query string, args ...interface{}) (*ridmodels.Subscription, error) {
	subs, err := c.process(ctx, query, args...)
	if err != nil {
		return nil, err // No need to Propagate this error as this stack layer does not add useful information
	}
	if len(subs) > 1 {
		return nil, stacktrace.NewError("Query returned %d subscriptions when only 0 or 1 was expected", len(subs))
	}
	if len(subs) == 0 {
		return nil, nil
	}
	return subs[0], nil
}

// MaxSubscriptionCountInCellsByOwner counts how many subscriptions the
// owner has in each one of these cells, and returns the number of subscriptions
// in the cell with the highest number of subscriptions.
func (c *subscriptionRepoV3) MaxSubscriptionCountInCellsByOwner(ctx context.Context, cells s2.CellUnion, owner dssmodels.Owner) (int, error) {
	// TODO:steeling this query is expensive. The standard defines the max sub
	// per "area", but area is loosely defined. Since we may not have to be so
	// strict we could keep this count in memory, (or in some other storage).
	var query = `
    SELECT
      IFNULL(MAX(subscriptions_per_cell_id), 0)
    FROM (
      SELECT
        COUNT(*) AS subscriptions_per_cell_id
      FROM (
      	SELECT unnest(cells) as cell_id
      	FROM subscriptions
      	WHERE owner = $1
      		AND ends_at >= $2
      )
      WHERE
        cell_id = ANY($3)
      GROUP BY cell_id
    )`

	cids := make([]int64, len(cells))
	for i, cell := range cells {
		cids[i] = int64(cell)
	}

	var pgCids pgtype.Int8Array

	if err := pgCids.Set(cids); err != nil {
		return 0, stacktrace.Propagate(err, "Failed to convert array to jackc/pgtype")
	}

	row := c.QueryRow(ctx, query, owner, c.clock.Now(), pgCids)
	var ret int
	err := row.Scan(&ret)
	return ret, stacktrace.Propagate(err, "Error scanning subscription count row")
}

// GetSubscription returns the subscription identified by "id".
// Returns nil, nil if not found
func (c *subscriptionRepoV3) GetSubscription(ctx context.Context, id dssmodels.ID) (*ridmodels.Subscription, error) {
	// TODO(steeling) we should enforce startTime and endTime to not be null at the DB level.
	var query = fmt.Sprintf(`
		SELECT %s FROM subscriptions
		WHERE id = $1`, subscriptionFieldsV3)
	uid, err := id.PgUUID()
	if err != nil {
		return nil, stacktrace.Propagate(err, "Failed to convert id to PgUUID")
	}
	return c.processOne(ctx, query, uid)
}

// UpdateSubscription updates the Subscription.. not yet implemented.
// Returns nil, nil if ID, version not found
func (c *subscriptionRepoV3) UpdateSubscription(ctx context.Context, s *ridmodels.Subscription) (*ridmodels.Subscription, error) {
	var (
		updateQuery = fmt.Sprintf(`
		UPDATE
		  subscriptions
		SET (%s) = ($1, $2, $3, $4, $5, $6, transaction_timestamp())
		WHERE id = $1 AND updated_at = $7
		RETURNING
			%s`, updateSubscriptionFieldsV3, subscriptionFieldsV3)
	)

	cids := make([]int64, len(s.Cells))

	for i, cell := range s.Cells {
		if err := geo.ValidateCell(cell); err != nil {
			return nil, stacktrace.Propagate(err, "Error validating cell")
		}
		cids[i] = int64(cell)
	}

	var pgCids pgtype.Int8Array
	err := pgCids.Set(cids)
	if err != nil {
		return nil, stacktrace.Propagate(err, "Failed to convert array to jackc/pgtype")
	}

	id, err := s.ID.PgUUID()
	if err != nil {
		return nil, stacktrace.Propagate(err, "Failed to convert id to PgUUID")
	}
	return c.processOne(ctx, updateQuery,
		id,
		s.URL,
		s.NotificationIndex,
		pgCids,
		s.StartTime,
		s.EndTime,
		s.Version.ToTimestamp())
}

// InsertSubscription inserts subscription into the store and returns
// the resulting subscription including its ID.
func (c *subscriptionRepoV3) InsertSubscription(ctx context.Context, s *ridmodels.Subscription) (*ridmodels.Subscription, error) {
	var (
		insertQuery = fmt.Sprintf(`
		INSERT INTO
		  subscriptions
		  (%s)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, transaction_timestamp())
		RETURNING
			%s`, subscriptionFieldsV3, subscriptionFieldsV3)
	)

	cids := make([]int64, len(s.Cells))

	for i, cell := range s.Cells {
		if err := geo.ValidateCell(cell); err != nil {
			return nil, stacktrace.Propagate(err, "Error validating cell")
		}
		cids[i] = int64(cell)
	}

	var pgCids pgtype.Int8Array
	err := pgCids.Set(cids)
	if err != nil {
		return nil, stacktrace.Propagate(err, "Failed to convert array to jackc/pgtype")
	}

	uid, err := s.ID.PgUUID()
	if err != nil {
		return nil, stacktrace.Propagate(err, "Failed to convert id to PgUUID")
	}
	return c.processOne(ctx, insertQuery,
		uid,
		s.Owner,
		s.URL,
		s.NotificationIndex,
		pgCids,
		s.StartTime,
		s.EndTime)
}

// DeleteSubscription deletes the subscription identified by ID.
// It must be done in a txn and the version verified.
// Returns nil, nil if ID, version not found
func (c *subscriptionRepoV3) DeleteSubscription(ctx context.Context, s *ridmodels.Subscription) (*ridmodels.Subscription, error) {
	var (
		query = fmt.Sprintf(`
		DELETE FROM
			subscriptions
		WHERE
			id = $1
			AND updated_at = $2
		RETURNING %s`, subscriptionFieldsV3)
	)
	id, err := s.ID.PgUUID()
	if err != nil {
		return nil, stacktrace.Propagate(err, "Failed to convert id to PgUUID")
	}
	return c.processOne(ctx, query, id, s.Version.ToTimestamp())
}

// UpdateNotificationIdxsInCells incremement the notification for each sub in the given cells.
func (c *subscriptionRepoV3) UpdateNotificationIdxsInCells(ctx context.Context, cells s2.CellUnion) ([]*ridmodels.Subscription, error) {
	var updateQuery = fmt.Sprintf(`
			UPDATE subscriptions
			SET notification_index = notification_index + 1
			WHERE
				cells && $1
				AND ends_at >= $2
			RETURNING %s`, subscriptionFieldsV3)

	cids := make([]int64, len(cells))
	for i, cell := range cells {
		cids[i] = int64(cell)
	}

	var pgCids pgtype.Int8Array
	err := pgCids.Set(cids)
	if err != nil {
		return nil, stacktrace.Propagate(err, "Failed to convert array to jackc/pgtype")
	}

	return c.process(
		ctx, updateQuery, pgCids, c.clock.Now())
}

// SearchSubscriptions returns all subscriptions in "cells".
func (c *subscriptionRepoV3) SearchSubscriptions(ctx context.Context, cells s2.CellUnion) ([]*ridmodels.Subscription, error) {
	var (
		query = fmt.Sprintf(`
			SELECT
				%s
			FROM
				subscriptions
			WHERE
				cells && $1
			AND
				ends_at >= $2
			LIMIT $3`, subscriptionFieldsV3)
	)

	if len(cells) == 0 {
		return nil, stacktrace.NewErrorWithCode(dsserr.BadRequest, "no location provided")
	}

	cids := make([]int64, len(cells))
	for i, cell := range cells {
		cids[i] = int64(cell)
	}

	var pgCids pgtype.Int8Array
	err := pgCids.Set(cids)
	if err != nil {
		return nil, stacktrace.Propagate(err, "Failed to convert array to jackc/pgtype")
	}

	return c.process(ctx, query, pgCids, c.clock.Now(), dssmodels.MaxResultLimit)
}

// SearchSubscriptionsByOwner returns all subscriptions in "cells".
func (c *subscriptionRepoV3) SearchSubscriptionsByOwner(ctx context.Context, cells s2.CellUnion, owner dssmodels.Owner) ([]*ridmodels.Subscription, error) {
	var (
		query = fmt.Sprintf(`
			SELECT
				%s
			FROM
				subscriptions
			WHERE
				cells && $1
			AND
				subscriptions.owner = $2
			AND
				ends_at >= $3
			LIMIT $4`, subscriptionFieldsV3)
	)

	if len(cells) == 0 {
		return nil, stacktrace.NewErrorWithCode(dsserr.BadRequest, "no location provided")
	}

	cids := make([]int64, len(cells))
	for i, cell := range cells {
		cids[i] = int64(cell)
	}

	var pgCids pgtype.Int8Array
	err := pgCids.Set(cids)
	if err != nil {
		return nil, stacktrace.Propagate(err, "Failed to convert array to jackc/pgtype")
	}

	return c.process(ctx, query, pgCids, owner, c.clock.Now(), dssmodels.MaxResultLimit)
}

// ListExpiredSubscriptions returns empty. We don't support this function in store v3.0 because db doesn't have 'writer' field.
func (c *subscriptionRepoV3) ListExpiredSubscriptions(ctx context.Context, writer string) ([]*ridmodels.Subscription, error) {
	return make([]*ridmodels.Subscription, 0), nil
}
