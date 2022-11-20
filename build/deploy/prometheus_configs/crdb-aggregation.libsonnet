{
  groups: [
    {
      name: 'rules/aggregation.rules',
      rules: [
        {
          record: 'node:capacity',
          expr: 'sum without(store) (capacity{app="cockroachdb"})',
        },
        {
          record: 'cluster:capacity',
          expr: 'sum without(instance) (node:capacity{app="cockroachdb"})',
        },
        {
          record: 'node:capacity_available',
          expr: 'sum without(store) (capacity_available{app="cockroachdb"})',
        },
        {
          record: 'cluster:capacity_available',
          expr: 'sum without(instance) (node:capacity_available{app="cockroachdb"})',
        },
        {
          record: 'capacity_available:ratio',
          expr: 'capacity_available{app="cockroachdb"} / capacity{app="cockroachdb"}',
        },
        {
          record: 'node:capacity_available:ratio',
          expr: 'node:capacity_available{app="cockroachdb"} / node:capacity{app="cockroachdb"}',
        },
        {
          record: 'cluster:capacity_available:ratio',
          expr: 'cluster:capacity_available{app="cockroachdb"} / cluster:capacity{app="cockroachdb"}',
        },
        {
          record: 'txn_durations_bucket:rate1m',
          expr: 'rate(txn_durations_bucket{app="cockroachdb"}[1m])',
        },
        {
          record: 'txn_durations:rate1m:quantile_50',
          expr: 'histogram_quantile(0.5, txn_durations_bucket:rate1m)',
        },
        {
          record: 'txn_durations:rate1m:quantile_75',
          expr: 'histogram_quantile(0.75, txn_durations_bucket:rate1m)',
        },
        {
          record: 'txn_durations:rate1m:quantile_90',
          expr: 'histogram_quantile(0.9, txn_durations_bucket:rate1m)',
        },
        {
          record: 'txn_durations:rate1m:quantile_95',
          expr: 'histogram_quantile(0.95, txn_durations_bucket:rate1m)',
        },
        {
          record: 'txn_durations:rate1m:quantile_99',
          expr: 'histogram_quantile(0.99, txn_durations_bucket:rate1m)',
        },
        {
          record: 'exec_latency_bucket:rate1m',
          expr: 'rate(exec_latency_bucket{app="cockroachdb"}[1m])',
        },
        {
          record: 'exec_latency:rate1m:quantile_50',
          expr: 'histogram_quantile(0.5, exec_latency_bucket:rate1m)',
        },
        {
          record: 'exec_latency:rate1m:quantile_75',
          expr: 'histogram_quantile(0.75, exec_latency_bucket:rate1m)',
        },
        {
          record: 'exec_latency:rate1m:quantile_90',
          expr: 'histogram_quantile(0.9, exec_latency_bucket:rate1m)',
        },
        {
          record: 'exec_latency:rate1m:quantile_95',
          expr: 'histogram_quantile(0.95, exec_latency_bucket:rate1m)',
        },
        {
          record: 'exec_latency:rate1m:quantile_99',
          expr: 'histogram_quantile(0.99, exec_latency_bucket:rate1m)',
        },
        {
          record: 'round_trip_latency_bucket:rate1m',
          expr: 'rate(round_trip_latency_bucket{app="cockroachdb"}[1m])',
        },
        {
          record: 'round_trip_latency:rate1m:quantile_50',
          expr: 'histogram_quantile(0.5, round_trip_latency_bucket:rate1m)',
        },
        {
          record: 'round_trip_latency:rate1m:quantile_75',
          expr: 'histogram_quantile(0.75, round_trip_latency_bucket:rate1m)',
        },
        {
          record: 'round_trip_latency:rate1m:quantile_90',
          expr: 'histogram_quantile(0.9, round_trip_latency_bucket:rate1m)',
        },
        {
          record: 'round_trip_latency:rate1m:quantile_95',
          expr: 'histogram_quantile(0.95, round_trip_latency_bucket:rate1m)',
        },
        {
          record: 'round_trip_latency:rate1m:quantile_99',
          expr: 'histogram_quantile(0.99, round_trip_latency_bucket:rate1m)',
        },
        {
          record: 'sql_exec_latency_bucket:rate1m',
          expr: 'rate(sql_exec_latency_bucket{app="cockroachdb"}[1m])',
        },
        {
          record: 'sql_exec_latency:rate1m:quantile_50',
          expr: 'histogram_quantile(0.5, sql_exec_latency_bucket:rate1m)',
        },
        {
          record: 'sql_exec_latency:rate1m:quantile_75',
          expr: 'histogram_quantile(0.75, sql_exec_latency_bucket:rate1m)',
        },
        {
          record: 'sql_exec_latency:rate1m:quantile_90',
          expr: 'histogram_quantile(0.9, sql_exec_latency_bucket:rate1m)',
        },
        {
          record: 'sql_exec_latency:rate1m:quantile_95',
          expr: 'histogram_quantile(0.95, sql_exec_latency_bucket:rate1m)',
        },
        {
          record: 'sql_exec_latency:rate1m:quantile_99',
          expr: 'histogram_quantile(0.99, sql_exec_latency_bucket:rate1m)',
        },
        {
          record: 'raft_process_logcommit_latency_bucket:rate1m',
          expr: 'rate(raft_process_logcommit_latency_bucket{app="cockroachdb"}[1m])',
        },
        {
          record: 'raft_process_logcommit_latency:rate1m:quantile_50',
          expr: 'histogram_quantile(0.5, raft_process_logcommit_latency_bucket:rate1m)',
        },
        {
          record: 'raft_process_logcommit_latency:rate1m:quantile_75',
          expr: 'histogram_quantile(0.75, raft_process_logcommit_latency_bucket:rate1m)',
        },
        {
          record: 'raft_process_logcommit_latency:rate1m:quantile_90',
          expr: 'histogram_quantile(0.9, raft_process_logcommit_latency_bucket:rate1m)',
        },
        {
          record: 'raft_process_logcommit_latency:rate1m:quantile_95',
          expr: 'histogram_quantile(0.95, raft_process_logcommit_latency_bucket:rate1m)',
        },
        {
          record: 'raft_process_logcommit_latency:rate1m:quantile_99',
          expr: 'histogram_quantile(0.99, raft_process_logcommit_latency_bucket:rate1m)',
        },
        {
          record: 'raft_process_commandcommit_latency_bucket:rate1m',
          expr: 'rate(raft_process_commandcommit_latency_bucket{app="cockroachdb"}[1m])',
        },
        {
          record: 'raft_process_commandcommit_latency:rate1m:quantile_50',
          expr: 'histogram_quantile(0.5, raft_process_commandcommit_latency_bucket:rate1m)',
        },
        {
          record: 'raft_process_commandcommit_latency:rate1m:quantile_75',
          expr: 'histogram_quantile(0.75, raft_process_commandcommit_latency_bucket:rate1m)',
        },
        {
          record: 'raft_process_commandcommit_latency:rate1m:quantile_90',
          expr: 'histogram_quantile(0.9, raft_process_commandcommit_latency_bucket:rate1m)',
        },
        {
          record: 'raft_process_commandcommit_latency:rate1m:quantile_95',
          expr: 'histogram_quantile(0.95, raft_process_commandcommit_latency_bucket:rate1m)',
        },
        {
          record: 'raft_process_commandcommit_latency:rate1m:quantile_99',
          expr: 'histogram_quantile(0.99, raft_process_commandcommit_latency_bucket:rate1m)',
        },
      ],
    },
  ],
}