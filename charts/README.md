# hyperfleet-sentinel

![Version: 1.0.0](https://img.shields.io/badge/Version-1.0.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.0.0-dev](https://img.shields.io/badge/AppVersion-0.0.0--dev-informational?style=flat-square)

HyperFleet Sentinel - Kubernetes service that polls HyperFleet API and publishes CloudEvents

**Homepage:** <https://github.com/openshift-hyperfleet/hyperfleet-sentinel>

## Installation

```bash
helm install hyperfleet-sentinel oci://REGISTRY/hyperfleet-sentinel \
  --set image.registry=REGISTRY \
  --set image.repository=ORG/hyperfleet-sentinel \
  --set image.tag=<version>
```

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| HyperFleet Team |  |  |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| replicaCount | int | `1` | Number of sentinel replicas |
| image.registry | string | `"CHANGE_ME"` | Container image registry (no default — must be set) |
| image.repository | string | `"CHANGE_ME"` | Container image repository (no default — must be set) |
| image.pullPolicy | string | `"Always"` | Image pull policy |
| image.tag | string | `""` | Image tag (no default — must be set via `--set image.tag=<version>`) |
| imagePullSecrets | list | `[]` | Secrets for pulling images from private registries |
| nameOverride | string | `""` | Override the chart name used in resource names |
| fullnameOverride | string | `""` | Override the full release name used in resource names |
| serviceAccount | object | `{"annotations":{},"create":true,"name":""}` | ServiceAccount configuration |
| serviceAccount.create | bool | `true` | Create a ServiceAccount for the sentinel |
| serviceAccount.annotations | object | `{}` | Annotations added to the ServiceAccount. For GCP Pub/Sub with Workload Identity Federation, use `gcloud projects add-iam-policy-binding` with the `principal://` format instead of annotations. |
| serviceAccount.name | string | `""` | Override the ServiceAccount name (defaults to the release fullname) |
| podAnnotations | object | `{}` | Additional annotations applied to all pods |
| podLabels | object | `{}` | Additional labels applied to all pods |
| podSecurityContext | object | `{"fsGroup":65532,"runAsNonRoot":true,"runAsUser":65532}` | Pod-level security context |
| podSecurityContext.fsGroup | int | `65532` | Filesystem group for volume mounts |
| podSecurityContext.runAsNonRoot | bool | `true` | Run all containers as non-root |
| podSecurityContext.runAsUser | int | `65532` | UID for all containers |
| securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true,"seccompProfile":{"type":"RuntimeDefault"}}` | Container-level security context |
| securityContext.readOnlyRootFilesystem | bool | `true` | Mount root filesystem as read-only |
| securityContext.allowPrivilegeEscalation | bool | `false` | Disallow privilege escalation |
| resources | object | `{"limits":{"cpu":"500m","memory":"512Mi"},"requests":{"cpu":"100m","memory":"128Mi"}}` | CPU and memory resource requests and limits |
| nodeSelector | object | `{}` | Node selector constraints for pod scheduling |
| tolerations | list | `[]` | Tolerations for pod scheduling |
| affinity | object | `{}` | Affinity rules for pod scheduling |
| extraVolumes | list | `[]` |  |
| extraVolumeMounts | list | `[]` |  |
| podDisruptionBudget.enabled | bool | `true` | Enable the PDB |
| podDisruptionBudget.maxUnavailable | int | `1` | Maximum number of pods that can be unavailable during disruption |
| config | object | `{"clients":{"hyperfleetApi":{"baseUrl":"http://hyperfleet-api:8000","timeout":"10s","version":"v1"}},"debugConfig":false,"log":{"format":"json","level":"info","output":"stdout"},"messageData":{"generation":"resource.generation","href":"resource.href","id":"resource.id","kind":"resource.kind"},"messageDecision":{"params":[{"expr":"condition(\"Reconciled\").last_updated_time","name":"ref_time"},{"expr":"condition(\"Reconciled\").status == \"True\"","name":"is_reconciled"},{"expr":"ref_time != \"\"","name":"has_ref_time"},{"expr":"!is_reconciled && resource.generation == 1","name":"is_new_resource"},{"expr":"resource.generation > condition(\"Reconciled\").observed_generation","name":"generation_mismatch"},{"expr":"is_reconciled && has_ref_time && now - timestamp(ref_time) > duration(\"30m\")","name":"reconciled_and_stale"},{"expr":"!is_reconciled && has_ref_time && now - timestamp(ref_time) > duration(\"10s\")","name":"not_reconciled_and_debounced"}],"result":"is_new_resource || generation_mismatch || reconciled_and_stale || not_reconciled_and_debounced"},"pollInterval":"5s","resourceSelector":[{"label":"shard","value":"1"}],"resourceType":"clusters","sentinel":{"name":"hyperfleet-sentinel-{{ .Values.config.resourceType }}"}}` | Sentinel application configuration. All settings in this section generate the ConfigMap consumed by the sentinel. |
| config.sentinel | object | `{"name":"hyperfleet-sentinel-{{ .Values.config.resourceType }}"}` | Sentinel identity settings |
| config.sentinel.name | string | `"hyperfleet-sentinel-{{ .Values.config.resourceType }}"` | Sentinel component name (templated with shard value when resource selector is used) |
| config.debugConfig | bool | `false` | Log merged configuration on startup for debugging |
| config.log | object | `{"format":"json","level":"info","output":"stdout"}` | Logging configuration |
| config.log.level | string | `"info"` | Log level (`debug`, `info`, `warn`, `error`) |
| config.log.format | string | `"json"` | Log format (`json` or `text`) |
| config.log.output | string | `"stdout"` | Log output destination |
| config.clients | object | `{"hyperfleetApi":{"baseUrl":"http://hyperfleet-api:8000","timeout":"10s","version":"v1"}}` | Client configuration |
| config.clients.hyperfleetApi | object | `{"baseUrl":"http://hyperfleet-api:8000","timeout":"10s","version":"v1"}` | HyperFleet API client settings |
| config.clients.hyperfleetApi.baseUrl | string | `"http://hyperfleet-api:8000"` | API base URL (use in-cluster service name) |
| config.clients.hyperfleetApi.version | string | `"v1"` | API version |
| config.clients.hyperfleetApi.timeout | string | `"10s"` | HTTP client timeout |
| config.resourceType | string | `"clusters"` | Resource type to watch (`clusters` or `nodepools`) |
| config.pollInterval | string | `"5s"` | How often to poll the API for resource updates |
| config.messageDecision | object | See values.yaml for default CEL expressions | CEL-based decision logic that determines whether to publish an event. `params` are named CEL expressions evaluated in dependency order. `result` is a boolean CEL expression using the params. |
| config.resourceSelector | list | `[{"label":"shard","value":"1"}]` | Resource selector for horizontal sharding. Deploy multiple sentinel instances with different shard values. |
| config.messageData | object | `{"generation":"resource.generation","href":"resource.href","id":"resource.id","kind":"resource.kind"}` | CloudEvents data payload configuration. Values are CEL expressions evaluated against the resource. |
| broker | object | `{"googlepubsub":{"createTopicIfMissing":false,"maxOutstandingMessages":1000,"numGoroutines":10,"projectId":"your-gcp-project-id"},"rabbitmq":{"exchangeType":"topic","url":"amqp://<USER>:<PASSWORD>@rabbitmq.hyperfleet-system.svc.cluster.local:5672/hyperfleet"},"topic":"{{ .Release.Namespace }}-{{ .Values.config.resourceType }}","type":"rabbitmq"}` | Broker configuration for event publishing. **WARNING:** Never commit real credentials to git. Use external secrets management (External Secrets Operator, Sealed Secrets, Vault). |
| broker.type | string | `"rabbitmq"` | Broker type (`rabbitmq` or `googlepubsub`). See the [broker library](https://github.com/openshift-hyperfleet/hyperfleet-broker). |
| broker.topic | string | `"{{ .Release.Namespace }}-{{ .Values.config.resourceType }}"` | Topic name for event publishing. Default uses Helm template `{namespace}-{resourceType}` for multi-tenant isolation. |
| broker.rabbitmq | object | `{"exchangeType":"topic","url":"amqp://<USER>:<PASSWORD>@rabbitmq.hyperfleet-system.svc.cluster.local:5672/hyperfleet"}` | RabbitMQ configuration |
| broker.rabbitmq.url | string | `"amqp://<USER>:<PASSWORD>@rabbitmq.hyperfleet-system.svc.cluster.local:5672/hyperfleet"` | Connection URL (`amqp://user:password@host:port/vhost`) |
| broker.rabbitmq.exchangeType | string | `"topic"` | Exchange type |
| broker.googlepubsub | object | `{"createTopicIfMissing":false,"maxOutstandingMessages":1000,"numGoroutines":10,"projectId":"your-gcp-project-id"}` | Google Pub/Sub configuration |
| broker.googlepubsub.projectId | string | `"your-gcp-project-id"` | GCP project ID (required when using Pub/Sub) |
| broker.googlepubsub.maxOutstandingMessages | int | `1000` | Maximum outstanding messages |
| broker.googlepubsub.numGoroutines | int | `10` | Number of subscriber goroutines |
| broker.googlepubsub.createTopicIfMissing | bool | `false` | Auto-create topic if it does not exist (use `false` in production) |
| monitoring | object | `{"podMonitoring":{"additionalLabels":{},"enabled":false,"interval":"30s","metricRelabeling":[]},"prometheusRule":{"additionalLabels":{},"alerts":{"sentinelPollStale":{"for":"1m","staleAfterSeconds":60}},"enabled":false,"namespace":""},"serviceMonitor":{"additionalLabels":{},"enabled":false,"honorLabels":true,"interval":"30s","metricRelabeling":[],"namespace":"","namespaceSelector":{},"scrapeTimeout":"10s"}}` | Monitoring and metrics configuration |
| monitoring.podMonitoring | object | `{"additionalLabels":{},"enabled":false,"interval":"30s","metricRelabeling":[]}` | PodMonitoring for Google Cloud Managed Prometheus (GMP) |
| monitoring.podMonitoring.enabled | bool | `false` | Create a PodMonitoring resource |
| monitoring.podMonitoring.interval | string | `"30s"` | Scrape interval |
| monitoring.podMonitoring.additionalLabels | object | `{}` | Additional labels for PodMonitoring discovery |
| monitoring.podMonitoring.metricRelabeling | list | `[]` | Metric relabel configs applied before ingestion |
| monitoring.serviceMonitor | object | `{"additionalLabels":{},"enabled":false,"honorLabels":true,"interval":"30s","metricRelabeling":[],"namespace":"","namespaceSelector":{},"scrapeTimeout":"10s"}` | ServiceMonitor for Prometheus Operator environments |
| monitoring.serviceMonitor.enabled | bool | `false` | Create a ServiceMonitor resource |
| monitoring.serviceMonitor.interval | string | `"30s"` | Scrape interval |
| monitoring.serviceMonitor.scrapeTimeout | string | `"10s"` | Scrape timeout (must be less than interval) |
| monitoring.serviceMonitor.additionalLabels | object | `{}` | Additional labels for ServiceMonitor discovery |
| monitoring.serviceMonitor.namespaceSelector | object | `{}` | Namespace selector for cross-namespace monitoring |
| monitoring.serviceMonitor.honorLabels | bool | `true` | Honor labels from the target to avoid overwriting |
| monitoring.serviceMonitor.metricRelabeling | list | `[]` | Metric relabel configs applied before ingestion |
| monitoring.serviceMonitor.namespace | string | `""` | Override the namespace where ServiceMonitor is created (defaults to release namespace) |
| monitoring.prometheusRule | object | `{"additionalLabels":{},"alerts":{"sentinelPollStale":{"for":"1m","staleAfterSeconds":60}},"enabled":false,"namespace":""}` | PrometheusRule for alerting (requires Prometheus Operator) |
| monitoring.prometheusRule.enabled | bool | `false` | Create PrometheusRule resources |
| monitoring.prometheusRule.namespace | string | `""` | Namespace to create the PrometheusRule in (defaults to release namespace) |
| monitoring.prometheusRule.additionalLabels | object | `{}` | Additional labels for PrometheusRule discovery |
| monitoring.prometheusRule.alerts | object | `{"sentinelPollStale":{"for":"1m","staleAfterSeconds":60}}` | Alert rule configuration |
| monitoring.prometheusRule.alerts.sentinelPollStale | object | `{"for":"1m","staleAfterSeconds":60}` | Poll staleness alert |
| monitoring.prometheusRule.alerts.sentinelPollStale.staleAfterSeconds | int | `60` | Seconds after which a poll is considered stale |
| monitoring.prometheusRule.alerts.sentinelPollStale.for | string | `"1m"` | Duration before the alert fires |
| tracing | object | `{"enabled":false,"otlpEndpoint":"","otlpProtocol":"grpc","propagators":"tracecontext,baggage","sampler":"parentbased_traceidratio","samplerArg":"1.0","serviceName":"hyperfleet-sentinel"}` | Distributed tracing configuration (OpenTelemetry) |
| tracing.enabled | bool | `false` | Enable trace export |
| tracing.serviceName | string | `"hyperfleet-sentinel"` | Service name reported in traces |
| tracing.otlpEndpoint | string | `""` | OTLP exporter endpoint (traces go to stdout when empty) |
| tracing.otlpProtocol | string | `"grpc"` | OTLP protocol (`grpc` or `http/protobuf`) |
| tracing.sampler | string | `"parentbased_traceidratio"` | Sampler type |
| tracing.samplerArg | string | `"1.0"` | Sampling rate (`1.0` for dev, `0.01` for production) |
| tracing.propagators | string | `"tracecontext,baggage"` | Context propagation formats |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs](https://github.com/norwoodj/helm-docs)
