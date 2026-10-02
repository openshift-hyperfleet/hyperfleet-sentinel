# Konflux build failure notifications

These Sentinel PipelineRuns send a Slack notification from a Tekton `finally`
task when `$(tasks.status)` is `Failed`: `hyperfleet-sentinel-on-push`,
`hyperfleet-sentinel-on-tag`, `hyperfleet-sentinel-chart-on-push`, and
`hyperfleet-sentinel-chart-on-tag`.

Each alert names the pipeline and links the repository, commit, and failed run.
Successful runs skip the notification task.
Runs rejected before the pipeline starts cannot execute a `finally` task.

The task reads key `hyperfleet-slack-webhook-url` from Secret
`hyperfleet-slack-webhook-notification-secret` in namespace `hyperfleet-tenant`.
HyperFleet manages this Secret in its own tenant namespace. The intended
destination is the team's `#hyperfleet-e2e-status` Slack channel. Keep the
webhook value out of source control, logs, and issue comments. Release
notifications use a separate integration; see the [notification runbook][runbook].

## Verify and troubleshoot

After an authorized rollout, trigger a controlled failed run and confirm that
the message arrives within a few minutes with the correct repository, pipeline,
commit, and working run link. Trigger or observe a successful updated run and
confirm it sends no build failure alert. A successful tag build can initiate a
release, so coordinate that test with the release owner.

If an expected alert is missing, open the PipelineRun in Konflux and inspect the
`slack-webhook-notification` final TaskRun and its logs. Check that the build
tenant Secret exists with the named key and that the task can mount it; inspect
the team's Secret configuration if it is managed by GitOps. A failed final task
is distinct from the build failure that triggered it. Also check whether the run reached
Tekton at all, since pre-start failures cannot produce this alert.

## Rotate the webhook

1. Coordinate a replacement webhook for the approved channel with the
   HyperFleet team. Determine whether its value is also used by release
   notifications.
2. Update the build tenant Secret through the team's Secret management process
   and its configuration source if one is used. If the value is shared,
   coordinate the release source update with RelEng.
3. After synchronization, verify only Secret metadata and key presence; never
   print the value. Confirm a controlled failed build delivers an alert. If
   shared, confirm a release notification still works.
4. Revoke the old webhook only after every consumer has been verified on the
   replacement.

[runbook]: https://github.com/openshift-hyperfleet/architecture/blob/main/hyperfleet/docs/release/operations/notifications.md
