package service

import "database/sql"

// scanUsageRow scans one grouped row into a UsageRow. includeDay is true for
// trend queries where the first selected column is date(m.created_at).
func scanUsageRow(scanner interface{ Scan(dest ...any) error }, dims []UsageDim, includeDay bool) (*UsageRow, error) {
	row := &UsageRow{Key: map[string]string{}}
	dest := make([]any, 0, len(dims)+8)
	if includeDay {
		dest = append(dest, &row.Day)
	}
	labels := make([]string, len(dims))
	for i := range labels {
		dest = append(dest, &labels[i])
	}
	var (
		input, output, total, hit, miss int64
		credit, cost                    float64
		msgCnt                          int64
	)
	dest = append(dest, &input, &output, &total, &hit, &miss, &credit, &cost, &msgCnt)
	if err := scanner.Scan(dest...); err != nil {
		return nil, err
	}
	for i, d := range dims {
		row.Key[string(d)] = labels[i]
	}
	row.Input, row.Output, row.Total = input, output, total
	row.CacheHit, row.CacheMiss = hit, miss
	row.Credit, row.CostUSD = credit, cost
	row.MessageCnt = msgCnt
	return row, nil
}

// scanUsageTotalsRow scans a single ungrouped aggregate row.
func scanUsageTotalsRow(row *sql.Row) (*UsageTotals, error) {
	var t UsageTotals
	if err := row.Scan(&t.Input, &t.Output, &t.Total, &t.CacheHit, &t.CacheMiss, &t.Credit, &t.CostUSD, &t.MessageCnt); err != nil {
		return nil, err
	}
	return &t, nil
}
