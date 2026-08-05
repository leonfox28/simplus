package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
)

const (
	ResourceLeaseOperation = "operation"
	ResourceLeaseCall      = "call"
)

var (
	ErrResourceGroupBusy            = errors.New("resource group is busy")
	ErrResourceLeaseMissing         = errors.New("resource group lease is missing or stale")
	ErrResourceLeaseReplay          = errors.New("operation id belongs to a different lease request")
	ErrResourceLeaseClosed          = errors.New("operation id belongs to a closed resource lease")
	ErrResourceLeaseStaleGeneration = errors.New("resource group generation is stale")
)

type ResourceLeaseAcquire struct {
	LeaseID                 string
	OperationID             string
	ResourceGroupID         string
	Kind                    string
	Purpose                 string
	Holder                  string
	ResourceGroupGeneration uint64
	MaxActiveCalls          int
	MaxConcurrentOperations int
	Now                     time.Time
	ExpiresAt               time.Time
}

type ResourceLease struct {
	LeaseID                 string
	OperationID             string
	ResourceGroupID         string
	Kind                    string
	Purpose                 string
	Holder                  string
	ResourceGroupGeneration uint64
	FencingToken            uint64
	CreatedAt               time.Time
	ExpiresAt               time.Time
}

func (set *Set) AcquireResourceGroupLease(ctx context.Context, request ResourceLeaseAcquire) (ResourceLease, bool, error) {
	if set == nil || set.Runtime == nil {
		return ResourceLease{}, false, fmt.Errorf("runtime database is not configured")
	}
	if err := validateResourceLeaseAcquire(request); err != nil {
		return ResourceLease{}, false, err
	}
	tx, err := set.Runtime.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ResourceLease{}, false, fmt.Errorf("begin resource lease transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	nowUnix := request.Now.UTC().Unix()
	if _, err := tx.ExecContext(ctx, `DELETE FROM resource_group_leases WHERE expires_at_unix <= ?`, nowUnix); err != nil {
		return ResourceLease{}, false, fmt.Errorf("expire resource leases: %w", err)
	}
	var latestGeneration int64
	err = tx.QueryRowContext(ctx, `
SELECT resource_group_generation FROM resource_group_fences WHERE resource_group_id = ?
`, request.ResourceGroupID).Scan(&latestGeneration)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return ResourceLease{}, false, fmt.Errorf("read resource group generation fence: %w", err)
	case request.ResourceGroupGeneration < uint64(latestGeneration):
		return ResourceLease{}, false, ErrResourceLeaseStaleGeneration
	case request.ResourceGroupGeneration > uint64(latestGeneration):
		if _, err := tx.ExecContext(ctx, `
DELETE FROM resource_group_leases
WHERE resource_group_id = ? AND resource_group_generation < ?
`, request.ResourceGroupID, request.ResourceGroupGeneration); err != nil {
			return ResourceLease{}, false, fmt.Errorf("fence stale-generation resource leases: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE resource_group_fences SET resource_group_generation = ? WHERE resource_group_id = ?
`, request.ResourceGroupGeneration, request.ResourceGroupID); err != nil {
			return ResourceLease{}, false, fmt.Errorf("advance resource group generation fence: %w", err)
		}
	}
	if operation, found, err := resourceLeaseOperationByID(ctx, tx, request.OperationID); err != nil {
		return ResourceLease{}, false, err
	} else if found {
		if operation.ResourceGroupID != request.ResourceGroupID || operation.Kind != request.Kind || operation.Purpose != request.Purpose ||
			operation.Holder != request.Holder || operation.ResourceGroupGeneration != request.ResourceGroupGeneration {
			return ResourceLease{}, false, ErrResourceLeaseReplay
		}
		existing, active, err := resourceLeaseByID(ctx, tx, operation.LeaseID)
		if err != nil {
			return ResourceLease{}, false, err
		}
		if !active {
			return ResourceLease{}, false, ErrResourceLeaseClosed
		}
		if err := tx.Commit(); err != nil {
			return ResourceLease{}, false, fmt.Errorf("commit replayed resource lease: %w", err)
		}
		return existing, true, nil
	}

	var operationCount, callCount int
	if err := tx.QueryRowContext(ctx, `
SELECT
    COALESCE(SUM(CASE WHEN lease_kind = 'operation' THEN 1 ELSE 0 END), 0),
    COALESCE(SUM(CASE WHEN lease_kind = 'call' THEN 1 ELSE 0 END), 0)
FROM resource_group_leases
WHERE resource_group_id = ? AND expires_at_unix > ?
`, request.ResourceGroupID, nowUnix).Scan(&operationCount, &callCount); err != nil {
		return ResourceLease{}, false, fmt.Errorf("count active resource leases: %w", err)
	}
	switch request.Kind {
	case ResourceLeaseOperation:
		if operationCount >= request.MaxConcurrentOperations || callCount > 0 {
			return ResourceLease{}, false, ErrResourceGroupBusy
		}
	case ResourceLeaseCall:
		if operationCount > 0 || callCount >= request.MaxActiveCalls {
			return ResourceLease{}, false, ErrResourceGroupBusy
		}
	default:
		return ResourceLease{}, false, fmt.Errorf("unsupported resource lease kind %q", request.Kind)
	}

	var fencingToken int64
	if err := tx.QueryRowContext(ctx, `
INSERT INTO resource_group_fences (resource_group_id, resource_group_generation, fencing_token)
VALUES (?, ?, 1)
ON CONFLICT(resource_group_id) DO UPDATE SET
    resource_group_generation = excluded.resource_group_generation,
    fencing_token = fencing_token + 1
RETURNING fencing_token
`, request.ResourceGroupID, request.ResourceGroupGeneration).Scan(&fencingToken); err != nil {
		return ResourceLease{}, false, fmt.Errorf("advance resource lease fence: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO resource_group_leases (
    lease_id, operation_id, resource_group_id, lease_kind, purpose, holder,
    resource_group_generation, fencing_token, created_at_unix, expires_at_unix
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, request.LeaseID, request.OperationID, request.ResourceGroupID, request.Kind, request.Purpose, request.Holder,
		request.ResourceGroupGeneration, fencingToken, nowUnix, request.ExpiresAt.UTC().Unix()); err != nil {
		return ResourceLease{}, false, fmt.Errorf("insert resource lease: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO resource_group_lease_operations (
    operation_id, lease_id, resource_group_id, lease_kind, purpose, holder,
    resource_group_generation, fencing_token, created_at_unix
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, request.OperationID, request.LeaseID, request.ResourceGroupID, request.Kind, request.Purpose, request.Holder,
		request.ResourceGroupGeneration, fencingToken, nowUnix); err != nil {
		return ResourceLease{}, false, fmt.Errorf("insert resource lease operation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ResourceLease{}, false, fmt.Errorf("commit resource lease: %w", err)
	}
	return ResourceLease{
		LeaseID: request.LeaseID, OperationID: request.OperationID, ResourceGroupID: request.ResourceGroupID, Kind: request.Kind,
		Purpose: request.Purpose, Holder: request.Holder, ResourceGroupGeneration: request.ResourceGroupGeneration,
		FencingToken: uint64(fencingToken), CreatedAt: request.Now.UTC(), ExpiresAt: request.ExpiresAt.UTC(),
	}, false, nil
}

func (set *Set) RenewResourceGroupLease(ctx context.Context, leaseID string, fencingToken uint64, now, expiresAt time.Time) (ResourceLease, error) {
	if set == nil || set.Runtime == nil {
		return ResourceLease{}, fmt.Errorf("runtime database is not configured")
	}
	if leaseID == "" || fencingToken == 0 || now.IsZero() || !expiresAt.After(now) {
		return ResourceLease{}, fmt.Errorf("invalid resource lease renewal")
	}
	result, err := set.Runtime.ExecContext(ctx, `
UPDATE resource_group_leases
SET expires_at_unix = ?
WHERE lease_id = ? AND fencing_token = ? AND expires_at_unix > ?
`, expiresAt.UTC().Unix(), leaseID, fencingToken, now.UTC().Unix())
	if err != nil {
		return ResourceLease{}, fmt.Errorf("renew resource lease: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return ResourceLease{}, fmt.Errorf("read resource lease renewal result: %w", err)
	}
	if changed != 1 {
		return ResourceLease{}, ErrResourceLeaseMissing
	}
	lease, found, err := resourceLeaseByID(ctx, set.Runtime, leaseID)
	if err != nil {
		return ResourceLease{}, err
	}
	if !found {
		return ResourceLease{}, ErrResourceLeaseMissing
	}
	return lease, nil
}

func (set *Set) ReleaseResourceGroupLease(ctx context.Context, leaseID string, fencingToken uint64) error {
	if set == nil || set.Runtime == nil {
		return fmt.Errorf("runtime database is not configured")
	}
	if leaseID == "" || fencingToken == 0 {
		return fmt.Errorf("invalid resource lease release")
	}
	result, err := set.Runtime.ExecContext(ctx, `DELETE FROM resource_group_leases WHERE lease_id = ? AND fencing_token = ?`, leaseID, fencingToken)
	if err != nil {
		return fmt.Errorf("release resource lease: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read resource lease release result: %w", err)
	}
	if changed != 1 {
		return ErrResourceLeaseMissing
	}
	return nil
}

func (set *Set) ActiveResourceGroupLeases(ctx context.Context, groupID string, now time.Time) ([]ResourceLease, error) {
	if set == nil || set.Runtime == nil {
		return nil, fmt.Errorf("runtime database is not configured")
	}
	rows, err := set.Runtime.QueryContext(ctx, `
SELECT lease_id, operation_id, resource_group_id, lease_kind, purpose, holder,
       resource_group_generation, fencing_token, created_at_unix, expires_at_unix
FROM resource_group_leases
WHERE resource_group_id = ? AND expires_at_unix > ?
ORDER BY fencing_token
`, groupID, now.UTC().Unix())
	if err != nil {
		return nil, fmt.Errorf("query active resource leases: %w", err)
	}
	defer rows.Close()
	var leases []ResourceLease
	for rows.Next() {
		lease, err := scanResourceLease(rows)
		if err != nil {
			return nil, err
		}
		leases = append(leases, lease)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active resource leases: %w", err)
	}
	return leases, nil
}

func validateResourceLeaseAcquire(request ResourceLeaseAcquire) error {
	if len(request.LeaseID) < 16 || len(request.LeaseID) > 128 || request.OperationID == "" || len(request.OperationID) > 128 ||
		request.ResourceGroupID == "" || len(request.ResourceGroupID) > 64 || request.Purpose == "" || len(request.Purpose) > 64 ||
		request.Holder == "" || len(request.Holder) > 128 || request.ResourceGroupGeneration == 0 || request.ResourceGroupGeneration > math.MaxInt64 || request.Now.IsZero() ||
		!request.ExpiresAt.After(request.Now) || request.ExpiresAt.Sub(request.Now) > 10*time.Minute ||
		request.MaxActiveCalls < 0 || request.MaxActiveCalls > 64 || request.MaxConcurrentOperations < 1 || request.MaxConcurrentOperations > 64 {
		return fmt.Errorf("invalid resource lease request")
	}
	if request.Kind != ResourceLeaseOperation && request.Kind != ResourceLeaseCall {
		return fmt.Errorf("invalid resource lease kind")
	}
	if request.Kind == ResourceLeaseCall && request.MaxActiveCalls == 0 {
		return fmt.Errorf("resource group does not allow calls")
	}
	return nil
}

type leaseScanner interface {
	Scan(...any) error
}

func resourceLeaseOperationByID(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, operationID string) (ResourceLease, bool, error) {
	var operation ResourceLease
	var generation, fencingToken uint64
	var createdAt int64
	err := queryer.QueryRowContext(ctx, `
SELECT lease_id, operation_id, resource_group_id, lease_kind, purpose, holder,
       resource_group_generation, fencing_token, created_at_unix
FROM resource_group_lease_operations WHERE operation_id = ?
`, operationID).Scan(&operation.LeaseID, &operation.OperationID, &operation.ResourceGroupID, &operation.Kind, &operation.Purpose,
		&operation.Holder, &generation, &fencingToken, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ResourceLease{}, false, nil
	}
	if err != nil {
		return ResourceLease{}, false, fmt.Errorf("read resource lease operation: %w", err)
	}
	operation.ResourceGroupGeneration = generation
	operation.FencingToken = fencingToken
	operation.CreatedAt = time.Unix(createdAt, 0).UTC()
	return operation, true, nil
}

func resourceLeaseByID(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, leaseID string) (ResourceLease, bool, error) {
	return scanOptionalResourceLease(queryer.QueryRowContext(ctx, `
SELECT lease_id, operation_id, resource_group_id, lease_kind, purpose, holder,
       resource_group_generation, fencing_token, created_at_unix, expires_at_unix
FROM resource_group_leases WHERE lease_id = ?
`, leaseID))
}

func scanOptionalResourceLease(scanner leaseScanner) (ResourceLease, bool, error) {
	lease, err := scanResourceLease(scanner)
	if errors.Is(err, sql.ErrNoRows) {
		return ResourceLease{}, false, nil
	}
	if err != nil {
		return ResourceLease{}, false, err
	}
	return lease, true, nil
}

func scanResourceLease(scanner leaseScanner) (ResourceLease, error) {
	var lease ResourceLease
	var generation, fencingToken uint64
	var createdAt, expiresAt int64
	if err := scanner.Scan(&lease.LeaseID, &lease.OperationID, &lease.ResourceGroupID, &lease.Kind, &lease.Purpose, &lease.Holder,
		&generation, &fencingToken, &createdAt, &expiresAt); err != nil {
		return ResourceLease{}, err
	}
	lease.ResourceGroupGeneration = generation
	lease.FencingToken = fencingToken
	lease.CreatedAt = time.Unix(createdAt, 0).UTC()
	lease.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	return lease, nil
}
