package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/mihomo"
)

func (set *Set) ListMihomoSubscriptions(ctx context.Context) ([]domain.Subscription, error) {
	rows, err := set.Core.QueryContext(ctx, `SELECT id, display_name, url_ciphertext, url_plaintext, url_hint, enabled, last_refresh_at_utc, last_refresh_status, node_count, last_error_code, created_at_utc, updated_at_utc FROM mihomo_subscriptions ORDER BY display_name, id`)
	if err != nil {
		return nil, fmt.Errorf("list Mihomo subscriptions: %w", err)
	}
	defer rows.Close()
	result := make([]domain.Subscription, 0)
	for rows.Next() {
		item, err := scanMihomoSubscription(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (set *Set) ReadMihomoSubscription(ctx context.Context, id string) (domain.Subscription, bool, error) {
	item, err := scanMihomoSubscription(set.Core.QueryRowContext(ctx, `SELECT id, display_name, url_ciphertext, url_plaintext, url_hint, enabled, last_refresh_at_utc, last_refresh_status, node_count, last_error_code, created_at_utc, updated_at_utc FROM mihomo_subscriptions WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return domain.Subscription{}, false, nil
	}
	if err != nil {
		return domain.Subscription{}, false, err
	}
	return item, true, nil
}

type rowScanner interface{ Scan(...any) error }

func scanMihomoSubscription(row rowScanner) (domain.Subscription, error) {
	var item domain.Subscription
	var enabled int
	var refreshAt sql.NullString
	var created, updated string
	if err := row.Scan(&item.ID, &item.DisplayName, &item.URLCiphertext, &item.URLPlaintext, &item.URLHint, &enabled, &refreshAt, &item.LastRefreshStatus, &item.NodeCount, &item.LastErrorCode, &created, &updated); err != nil {
		return item, err
	}
	item.Enabled = enabled == 1
	var err error
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return item, fmt.Errorf("parse Mihomo subscription creation time: %w", err)
	}
	item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return item, fmt.Errorf("parse Mihomo subscription update time: %w", err)
	}
	if refreshAt.Valid {
		item.LastRefreshAt, err = time.Parse(time.RFC3339Nano, refreshAt.String)
		if err != nil {
			return item, fmt.Errorf("parse Mihomo subscription refresh time: %w", err)
		}
	}
	return item, nil
}

func (set *Set) UpsertMihomoSubscription(ctx context.Context, item domain.Subscription) error {
	nowCreated := item.CreatedAt.UTC().Format(time.RFC3339Nano)
	nowUpdated := item.UpdatedAt.UTC().Format(time.RFC3339Nano)
	_, err := set.Core.ExecContext(ctx, `INSERT INTO mihomo_subscriptions (id, display_name, url_ciphertext, url_plaintext, url_hint, enabled, last_refresh_at_utc, last_refresh_status, node_count, last_error_code, created_at_utc, updated_at_utc) VALUES (?, ?, ?, ?, ?, ?, NULL, 'never', 0, '', ?, ?) ON CONFLICT(id) DO UPDATE SET display_name=excluded.display_name, url_ciphertext=excluded.url_ciphertext, url_plaintext=excluded.url_plaintext, url_hint=excluded.url_hint, enabled=excluded.enabled, updated_at_utc=excluded.updated_at_utc`, item.ID, item.DisplayName, item.URLCiphertext, item.URLPlaintext, item.URLHint, boolInt(item.Enabled), nowCreated, nowUpdated)
	if err != nil {
		return fmt.Errorf("upsert Mihomo subscription: %w", err)
	}
	return nil
}

func (set *Set) DeleteMihomoSubscription(ctx context.Context, id string) (bool, error) {
	result, err := set.Core.ExecContext(ctx, `DELETE FROM mihomo_subscriptions WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("delete Mihomo subscription: %w", err)
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func (set *Set) ReplaceMihomoSubscriptionNodes(ctx context.Context, subscriptionID string, nodes []domain.Node, refreshedAt time.Time, status, errorCode string) error {
	tx, err := set.Core.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM mihomo_subscription_nodes WHERE subscription_id = ?`, subscriptionID); err != nil {
		return err
	}
	for _, node := range nodes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO mihomo_subscription_nodes (subscription_id, node_id, display_name, kind, proxy_yaml, country_code, country_name) VALUES (?, ?, ?, ?, ?, ?, ?)`, subscriptionID, node.ID, node.DisplayName, node.Kind, node.ProxyYAML, node.CountryCode, node.CountryName); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE mihomo_subscriptions SET last_refresh_at_utc=?, last_refresh_status=?, node_count=?, last_error_code=?, updated_at_utc=? WHERE id=?`, refreshedAt.UTC().Format(time.RFC3339Nano), status, len(nodes), errorCode, refreshedAt.UTC().Format(time.RFC3339Nano), subscriptionID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return fmt.Errorf("Mihomo subscription not found")
	}
	return tx.Commit()
}

func (set *Set) ListMihomoSubscriptionNodes(ctx context.Context, subscriptionID string) ([]domain.Node, error) {
	rows, err := set.Core.QueryContext(ctx, `SELECT subscription_id, node_id, display_name, kind, proxy_yaml, country_code, country_name FROM mihomo_subscription_nodes WHERE subscription_id=? ORDER BY display_name, node_id`, subscriptionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Node, 0)
	for rows.Next() {
		var node domain.Node
		if err := rows.Scan(&node.SubscriptionID, &node.ID, &node.DisplayName, &node.Kind, &node.ProxyYAML, &node.CountryCode, &node.CountryName); err != nil {
			return nil, err
		}
		result = append(result, node)
	}
	return result, rows.Err()
}

func (set *Set) MarkMihomoSubscriptionRefreshFailure(ctx context.Context, subscriptionID, errorCode string, refreshedAt time.Time) error {
	result, err := set.Core.ExecContext(ctx, `UPDATE mihomo_subscriptions SET last_refresh_at_utc=?, last_refresh_status='failed', last_error_code=?, updated_at_utc=? WHERE id=?`, refreshedAt.UTC().Format(time.RFC3339Nano), errorCode, refreshedAt.UTC().Format(time.RFC3339Nano), subscriptionID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("Mihomo subscription not found")
	}
	return nil
}

func (set *Set) ListMihomoEgressProfiles(ctx context.Context) ([]domain.EgressProfile, error) {
	rows, err := set.Core.QueryContext(ctx, `SELECT id, display_name, subscription_id, line_id, selection_type, selected_node_id, selected_country_code, source_cidr, enabled, created_at_utc, updated_at_utc FROM mihomo_egress_profiles ORDER BY display_name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.EgressProfile, 0)
	for rows.Next() {
		var item domain.EgressProfile
		var enabled int
		var created, updated string
		if err := rows.Scan(&item.ID, &item.DisplayName, &item.SubscriptionID, &item.LineID, &item.SelectionType, &item.SelectedNodeID, &item.SelectedCountryCode, &item.SourceCIDR, &enabled, &created, &updated); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		var err error
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (set *Set) UpsertMihomoEgressProfile(ctx context.Context, item domain.EgressProfile) error {
	_, err := set.Core.ExecContext(ctx, `INSERT INTO mihomo_egress_profiles (id,display_name,subscription_id,line_id,selection_type,selected_node_id,selected_country_code,source_cidr,enabled,created_at_utc,updated_at_utc) VALUES (?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET display_name=excluded.display_name,subscription_id=excluded.subscription_id,line_id=excluded.line_id,selection_type=excluded.selection_type,selected_node_id=excluded.selected_node_id,selected_country_code=excluded.selected_country_code,source_cidr=excluded.source_cidr,enabled=excluded.enabled,updated_at_utc=excluded.updated_at_utc`, item.ID, item.DisplayName, item.SubscriptionID, item.LineID, item.SelectionType, item.SelectedNodeID, item.SelectedCountryCode, item.SourceCIDR, boolInt(item.Enabled), item.CreatedAt.UTC().Format(time.RFC3339Nano), item.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (set *Set) DeleteMihomoEgressProfile(ctx context.Context, id string) (bool, error) {
	result, err := set.Core.ExecContext(ctx, `DELETE FROM mihomo_egress_profiles WHERE id=?`, id)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func (set *Set) ReadMihomoRuntimeSelection(ctx context.Context) (selected, running string, err error) {
	err = set.Core.QueryRowContext(ctx, `SELECT selected_subscription_id, running_subscription_id FROM mihomo_runtime_selection WHERE singleton=1`).Scan(&selected, &running)
	if err != nil {
		return "", "", fmt.Errorf("read Mihomo runtime selection: %w", err)
	}
	return selected, running, nil
}

func (set *Set) WriteMihomoSelectedSubscription(ctx context.Context, id string, at time.Time) error {
	result, err := set.Core.ExecContext(ctx, `UPDATE mihomo_runtime_selection SET selected_subscription_id=?, updated_at_utc=? WHERE singleton=1`, id, at.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("write selected Mihomo subscription: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return fmt.Errorf("write selected Mihomo subscription: singleton missing")
	}
	return nil
}

func (set *Set) WriteMihomoRunningSubscription(ctx context.Context, id string, at time.Time) error {
	result, err := set.Core.ExecContext(ctx, `UPDATE mihomo_runtime_selection SET running_subscription_id=?, updated_at_utc=? WHERE singleton=1`, id, at.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("write running Mihomo subscription: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return fmt.Errorf("write running Mihomo subscription: singleton missing")
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
