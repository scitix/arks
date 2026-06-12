# ArksApplication CRD Usage Guide

## Overview

`ArksApplication` is the canonical inference CRD. It supports both unified
(single-role) and disaggregated (prefill/decode-separated) deployment
topologies via `spec.mode`, with optional router fronting in either topology.

The legacy `ArksDisaggregatedApplication` CRD is frozen and kept for backward
compatibility. New workloads should use `ArksApplication`.

## Modes

| `spec.mode` | Description | Roles |
|-------------|-------------|-------|
| `unified` (default) | One role (`unified`) runs the full inference pipeline | `unified` [+ `router`] |
| `disaggregated` | Prefill / decode separated, fronted by a router | `prefill` + `decode` + `router` |

If `spec.mode` is omitted, the API server defaults it to `unified`.

## Spec Structure

```yaml
spec:
  mode: unified | disaggregated      # default: unified
  runtime: vllm | sglang | dynamo     # default: vllm
  runtimeImage: <image>                # shared by unified/prefill/decode
  routerImage: <image>                 # used when router is enabled
  runtimeImagePullSecrets: [...]
  model: {name: <arksmodel-name>}
  servedModelName: <string>
  podGroupPolicy: {...}                # gang scheduling
  coordinationPolicy: {...}            # disaggregated mode only

  # Role workloads — required per mode (CEL-validated)
  unified: {replicas, size, leaderCommandOverride, workerCommandOverride, runtimeCommonArgs, instanceSpec}
  prefill: {replicas, size, leaderCommandOverride, workerCommandOverride, runtimeCommonArgs, instanceSpec}
  decode:  {replicas, size, leaderCommandOverride, workerCommandOverride, runtimeCommonArgs, instanceSpec}

  # Router — presence enables it (no enabled flag)
  router:  {replicas, commandOverride, port, metricPort, routerArgs, instanceSpec}
```

**Command override semantics** (`leaderCommandOverride` / `workerCommandOverride`
on workload roles, `commandOverride` on router): when set, the user-provided
command replaces the controller-generated default. The original generated
command is exposed to the container as `ARKS_LEADER_COMMAND`,
`ARKS_WORKER_COMMAND`, or `ARKS_ROUTER_COMMAND` respectively, so the
override script can compose on top of it.

**Validation (CEL, enforced by API server):**

| Mode | Required fields |
|------|-----------------|
| `unified` | `spec.unified` |
| `disaggregated` | `spec.prefill`, `spec.decode`, `spec.router` |

Missing required fields cause `kubectl apply` to fail immediately with a clear
message.

## Mode 1 — Unified (single role)

```yaml
apiVersion: arks.ai/v1
kind: ArksApplication
metadata:
  name: qwen-demo
spec:
  mode: unified
  runtime: sglang
  runtimeImage: "registry-ap-southeast.scitix.ai/k8s/sglang:v0.5.2-cu126"
  model:
    name: qwen-0.5b
  servedModelName: qwen-0.5b
  unified:
    replicas: 1
    size: 1
    runtimeCommonArgs:
      - --dtype=half
      - --mem-fraction-static
      - "0.5"
      - --tp
      - "1"
    instanceSpec:
      terminationGracePeriodSeconds: 2
      volumeMounts:
        - {name: shm, mountPath: /dev/shm}
      volumes:
        - {name: shm, emptyDir: {medium: Memory}}
      resources:
        limits:   {nvidia.com/gpu: "1"}
        requests: {nvidia.com/gpu: "1"}
```

**Result:** 1 pod running the inference engine. Service routes traffic directly
to the engine.

## Mode 2 — Unified + Router

Front the unified pods with a router by adding `spec.router`. No `enabled` flag —
*presence* of `spec.router` turns it on.

```yaml
spec:
  mode: unified
  runtime: sglang
  runtimeImage: "registry-ap-southeast.scitix.ai/k8s/sglang:v0.5.2-cu126"
  model: {name: qwen-0.5b}
  servedModelName: qwen-0.5b
  unified:
    replicas: 2
    size: 1
    runtimeCommonArgs: [--dtype=half, --tp, "1"]
    instanceSpec:
      resources: {limits: {nvidia.com/gpu: "1"}, requests: {nvidia.com/gpu: "1"}}
      volumeMounts: [{name: shm, mountPath: /dev/shm}]
      volumes: [{name: shm, emptyDir: {medium: Memory}}]
  router:
    replicas: 1
    instanceSpec:
      resources: {limits: {cpu: "1", memory: 2Gi}, requests: {cpu: 500m, memory: 1Gi}}
```

**Result:** 2 engine pods + 1 router pod. Service selector points at the router.

To remove the router later, delete the `router:` block.

## Mode 3 — Unified Distributed (multi-GPU / multi-node)

Set `unified.size > 1` to spread a single inference instance across multiple
pods via LeaderWorkerSet.

```yaml
spec:
  mode: unified
  runtime: sglang
  runtimeImage: "registry-ap-southeast.scitix.ai/k8s/sglang:v0.5.2-cu126"
  model: {name: qwen-32b}
  unified:
    replicas: 1
    size: 2                       # 1 leader + 1 worker form a single instance
    runtimeCommonArgs: [--tp, "2"]
    instanceSpec:
      resources: {limits: {nvidia.com/gpu: "1"}, requests: {nvidia.com/gpu: "1"}}
```

## Mode 4 — Disaggregated (Prefill + Decode + Router)

Prefill and decode roles run independently. The router is mandatory.

```yaml
spec:
  mode: disaggregated
  runtime: sglang
  runtimeImage: "registry-ap-southeast.scitix.ai/k8s/sglang:v0.5.2-cu126"
  model: {name: qwen-0.5b}
  prefill:
    replicas: 1
    size: 1
    runtimeCommonArgs: [--disaggregation-mode, prefill, --tp, "1"]
    instanceSpec:
      resources: {limits: {nvidia.com/gpu: "1"}, requests: {nvidia.com/gpu: "1"}}
  decode:
    replicas: 1
    size: 1
    runtimeCommonArgs: [--disaggregation-mode, decode, --tp, "1"]
    instanceSpec:
      resources: {limits: {nvidia.com/gpu: "1"}, requests: {nvidia.com/gpu: "1"}}
  router:
    replicas: 1
    instanceSpec:
      resources: {limits: {cpu: "1", memory: 2Gi}, requests: {cpu: 500m, memory: 1Gi}}
```

## Mode 4b — Disaggregated with Heterogeneous Prefill Groups

When different request shapes want different engine configurations (for
example one prefill tuned for fast TTFT and another for long contexts),
replace `spec.prefill` with `spec.prefillGroups`. Each group is rendered as
its own role (`prefill-<name>`) and LeaderWorkerSet with independent
parallelism args, resources, and — optionally — its own model (for example a
pre-sharded weight copy bound to that group's parallel layout). All groups
serve behind the same router.

```yaml
spec:
  mode: disaggregated
  runtime: sglang
  model: {name: glm5-fp8}
  prefillGroups:
    - name: fast                      # single-node tp8 + dp attention
      model: {name: glm5-fp8-sharded-tp8dp8}   # optional per-group model
      replicas: 1
      size: 1
      runtimeCommonArgs: [--tp, "8", --dp, "8", --enable-dp-attention]
      instanceSpec:
        resources: {limits: {nvidia.com/gpu: "8"}}
    - name: long                      # two-node tp16 for long contexts
      replicas: 1
      size: 2
      runtimeCommonArgs: [--tp, "16"]
      instanceSpec:
        resources: {limits: {nvidia.com/gpu: "8"}}
  decode: {...}
  router: {...}
```

Rules and behavior:
- `spec.prefill` and `spec.prefillGroups` are mutually exclusive; in
  disaggregated mode exactly one of them is required. Existing single-prefill
  applications keep working unchanged (and keep their workload names, so an
  upgrade does not recreate pods).
- Group names must be DNS-1123 labels (max 24 chars) and unique.
- Name budget: derived workload names follow `<app>-prefill-<group>` (plus
  pod suffixes added by LWS), and Kubernetes caps DNS labels at 63
  characters. With a long application name plus a long group name the
  derived names can exceed the cap, so keep `len(app) + len(group)` roughly
  under 40 characters.
- Every group's pods carry the shared `arks.ai/role=prefill` label (so the
  router's service discovery matches all groups as one pool) plus
  `arks.ai/worker-group=<name>` for group-aware routing policies.
- A group without `model` uses `spec.model`. A per-group model referencing an
  existing PVC can be declared as an ArksModel without `spec.source` ("model
  is in existing storage").
- `status.prefillGroups[]` reports per-group replica counts;
  `status.prefill` holds the sum across groups.
- `spec.coordinationPolicy` is not yet supported together with
  `prefillGroups`.

See `config/samples/arks_v1_arksapplication_prefill_groups.yaml` for a
complete 2P1D example.

## Custom Router (non-sglang runtimes, unified mode only)

The default router image is the sglang router and is only valid when
`spec.runtime = sglang`. For other runtimes (`vllm` / `dynamo`) or a custom
router binary in **unified** mode, supply both `spec.routerImage` and
`spec.router.commandOverride`:

Two equivalent ways to write the override:

```yaml
# (a) put the whole launch script inside commandOverride; leave routerArgs empty
spec:
  router:
    commandOverride: ["/bin/sh", "-c", "python3 -m my_router --port 8080"]
    # routerArgs: []   # omit
```

```yaml
# (b) put the binary in commandOverride, the CLI flags in routerArgs
#     (commandOverride -> Container.Command, routerArgs -> Container.Args)
spec:
  router:
    commandOverride: ["python3", "-m", "my_router"]
    routerArgs: ["--port", "8080"]
```

Both yield `python3 -m my_router --port 8080` inside the container.

> Note: with the `/bin/sh -c "script"` shell form, anything in `routerArgs`
> becomes the shell's positional parameters (`$0`, `$1`, ...), not part of
> the script. Use form (a) and put your flags inside the script, or use
> form (b) without `-c`.

Validation rules:
- `unified + router + runtime != sglang` requires both `spec.routerImage`
  and `spec.router.commandOverride`.
- `disaggregated` mode currently only supports `runtime: sglang`. The
  controller rejects disaggregated CRs with non-sglang runtimes at the
  validate step.

When `spec.router.commandOverride` is set, the controller-generated default
sglang router command (if applicable) is exposed to the router container as
the `ARKS_ROUTER_COMMAND` environment variable, so an override script can
compose on top of the original command.

## CoordinationPolicy (disaggregated only)

Coordinates scaling and rolling updates between `prefill` and `decode` so they
do not drift apart. Only takes effect when `spec.mode = disaggregated`.

```yaml
spec:
  mode: disaggregated
  coordinationPolicy:
    scaling:
      maxSkew: "10%"
      progression: OrderScheduled    # or OrderReady
    rollingUpdate:
      maxSkew: "5%"
      maxUnavailable: "10%"
      partition: "0%"
```

## In-place Mode Switching

`spec.mode` and `spec.router` are mutable on a live CR. The controller drives a
`TrafficTarget` state machine to swap traffic when components become ready.

| Transition | What happens | Service availability |
|------------|--------------|----------------------|
| `unified` → `unified + router` | Router rolled out in parallel; Service selector switched to router when ready; engine kept | Near zero-downtime |
| `unified + router` → `unified` | Router role dropped; Service selector swap to engine pods on the same reconcile loop. The router Deployment terminates in parallel with the Service-selector swap, so a brief endpoints gap is possible. The engine pods stay alive throughout, so most traffic continues uninterrupted | Near zero-downtime (brief endpoints gap possible) |
| `unified` ↔ `disaggregated` | Old role set torn down; new role set created; `Status.TrafficTargetReady=False, Reason=ModeSwitching` until the new mode's target is ready | Interrupted (`status.trafficTarget=pending`) |

`status.trafficTarget` reflects which role the Service currently routes to:

| Value | Meaning |
|-------|---------|
| `engine` | Service points at unified-role pods (direct, no router) |
| `router` | Service points at router pods (proxied to engine / prefill / decode) |
| `pending` | Transitional; waiting for the target to become ready |

## Status

```
status:
  phase: Running | Pending | Failed | ...
  mode: unified | disaggregated      # last stable mode (used to detect mode-switch)
  trafficTarget: engine | router | pending
  replicas, readyReplicas, updatedReplicas
  unified: {replicas, readyReplicas, updatedReplicas}    # filled in unified mode
  prefill: {...}                                          # filled in disaggregated mode
  decode:  {...}                                          # filled in disaggregated mode
  router:  {...}                                          # filled when router enabled
  conditions: [Loaded, Ready, TrafficTargetReady, ...]
```

`kubectl get arksapplications`:

```
NAME       PHASE     MODE          READY   AGE
qwen-demo  Running   unified       2/2     5m
pd-demo    Running   disaggregated 3/3     12m
```

## Labels and Service Selector

Pods created by `ArksApplication` carry:

- `arks.ai/application=<name>`
- `arks.ai/model=<model name>`
- `arks.ai/role=unified|router|prefill|decode`
- `arks.ai/work-load-role=leader|worker`

The application Service selects pods via `arks.ai/role=<currentTrafficTarget>`.

The legacy `arks.ai/disaggregation-role` label is reserved for
`ArksDisaggregatedApplication` pods only — `ArksApplication` does **not** use
that key.

## Migrating from ArksDisaggregatedApplication

Field-by-field mapping for users on the legacy PD CRD:

| Legacy `ArksDisaggregatedApplication.spec` | New `ArksApplication.spec` |
|-------------------------------------------|----------------------------|
| `runtime` | `runtime` |
| `runtimeImage` | `runtimeImage` |
| `routerImage` | `routerImage` |
| `model` | `model` |
| `servedModelName` | `servedModelName` |
| `podGroupPolicy` | `podGroupPolicy` |
| `prefill.{replicas, size, ...}` | `prefill.{replicas, size, ...}` |
| `decode.{replicas, size, ...}` | `decode.{replicas, size, ...}` |
| `router.{replicas, commandOverride, ...}` | `router.{replicas, commandOverride, ...}` |
| *(N/A)* | `mode: disaggregated` |
| *(N/A)* | `coordinationPolicy` |

Migration creates a new `ArksApplication` resource. The legacy CRD stays
frozen and continues to work for existing CRs without modification.
