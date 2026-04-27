# Runbook: media audit_unavailable response

## Trigger

Grafana/Prometheus alert fires for sustained non-success media malware audit writes:

```promql
sum by (service, result) (increase(media_audit_write_attempts_total{service="media",result!="success"}[5m])) > 0
```

## Meaning

Media service detected malware during upload, but failed to append the required audit event within the bounded retry path. In this state the service **must fail closed** and return:

- HTTP `503`
- error code `audit_unavailable`
- message `Upload service temporarily unavailable, please retry.`

The 503 response must not reveal the malware verdict.

## Immediate checks

1. Open Grafana Explore / Prometheus and inspect:
   - `increase(media_audit_write_attempts_total{service="media",result="timeout"}[15m])`
   - `increase(media_audit_write_attempts_total{service="media",result="transient_error"}[15m])`
   - `histogram_quantile(0.95, sum by (le) (rate(media_audit_write_duration_seconds_bucket{service="media"}[15m])))`
2. Check media service logs for:
   - `event=audit_persistent_failure`
   - fields: `media_id`, `user_id`, `virus_name`, `attempts_count`
3. Verify PostgreSQL health for audit writes:
   - connection saturation
   - latency / lock waits on `audit_log`
   - errors in media service store layer
4. Confirm ClamAV is healthy separately. **Do not** treat this alert as a scan failure signal unless scan errors also spike.

## Expected behavior while incident is active

- Malware-positive uploads may return 503 instead of 422.
- Clean uploads should continue to work normally.
- No file should be persisted to R2 or DB on the 503 malware/audit path.

## Mitigation

1. Restore DB/audit-log write path health.
2. Watch the alert query until non-success attempts stop increasing.
3. Confirm `media_audit_write_attempts_total{result="success"}` resumes increasing for malware test uploads.
4. If needed, run a controlled EICAR smoke test through gateway/media after recovery and verify:
   - 422 `virus_detected` is returned
   - audit row is written
   - no upload object is created in R2

## Escalation

If the alert persists longer than 15 minutes:

- escalate to backend on-call
- include latest `audit_persistent_failure` log lines
- include DB saturation / latency evidence
- note whether failures are `timeout` or `transient_error`
