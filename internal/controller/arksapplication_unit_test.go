package controller

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	arksv1 "github.com/arks-ai/arks/api/v1"
)

func TestGenerateRBGSUnifiedWithoutRouter(t *testing.T) {
	reconciler := &ArksApplicationReconciler{}
	application := newTestArksApplication()
	model := newTestArksModel()

	rbgs, err := reconciler.generateRBGS(context.Background(), application, testModelsFor(application, model))
	if err != nil {
		t.Fatalf("generateRBGS returned error: %v", err)
	}

	if len(rbgs.Spec.Template.Roles) != 1 {
		t.Fatalf("expected 1 role, got %d", len(rbgs.Spec.Template.Roles))
	}
	if rbgs.Spec.Template.Roles[0].Name != "unified" {
		t.Fatalf("expected unified role, got %s", rbgs.Spec.Template.Roles[0].Name)
	}
	if len(rbgs.Spec.Template.CoordinationRequirements) != 0 {
		t.Fatalf("expected no coordination requirements, got %d", len(rbgs.Spec.Template.CoordinationRequirements))
	}
}

func TestGenerateRBGSUnifiedWithRouter(t *testing.T) {
	reconciler := &ArksApplicationReconciler{}
	application := newTestArksApplication()
	application.Spec.Runtime = string(arksv1.ArksRuntimeSGLang)
	application.Spec.Router = &arksv1.ArksApplicationRouter{
		InstanceSpec: arksv1.ArksInstanceSpec{ServiceAccountName: "router-sa"},
	}
	model := newTestArksModel()

	rbgs, err := reconciler.generateRBGS(context.Background(), application, testModelsFor(application, model))
	if err != nil {
		t.Fatalf("generateRBGS returned error: %v", err)
	}

	if len(rbgs.Spec.Template.Roles) != 2 {
		t.Fatalf("expected 2 roles, got %d", len(rbgs.Spec.Template.Roles))
	}
	if rbgs.Spec.Template.Roles[0].Name != "unified" {
		t.Fatalf("expected first role unified, got %s", rbgs.Spec.Template.Roles[0].Name)
	}
	if rbgs.Spec.Template.Roles[1].Name != "router" {
		t.Fatalf("expected second role router, got %s", rbgs.Spec.Template.Roles[1].Name)
	}
}

func TestGenerateRBGSDisaggregatedWithCoordination(t *testing.T) {
	reconciler := &ArksApplicationReconciler{}
	application := newTestArksApplication()
	application.Spec.Mode = arksv1.ArksApplicationModeDisaggregated
	application.Spec.Runtime = string(arksv1.ArksRuntimeSGLang)
	application.Spec.Unified = nil
	application.Spec.Router = &arksv1.ArksApplicationRouter{
		InstanceSpec: arksv1.ArksInstanceSpec{ServiceAccountName: "router-sa"},
	}
	application.Spec.Prefill = &arksv1.ArksApplicationWorkload{
		Replicas: ptrInt32(2),
		Size:     1,
	}
	application.Spec.Decode = &arksv1.ArksApplicationWorkload{
		Replicas: ptrInt32(4),
		Size:     1,
	}
	application.Spec.CoordinationPolicy = &arksv1.CoordinationPolicy{
		Scaling: &arksv1.ScalingCoordination{
			MaxSkew:     "10%",
			Progression: "OrderReady",
		},
	}
	model := newTestArksModel()

	rbgs, err := reconciler.generateRBGS(context.Background(), application, testModelsFor(application, model))
	if err != nil {
		t.Fatalf("generateRBGS returned error: %v", err)
	}

	if len(rbgs.Spec.Template.Roles) != 3 {
		t.Fatalf("expected 3 roles, got %d", len(rbgs.Spec.Template.Roles))
	}
	if rbgs.Spec.Template.Roles[0].Name != "router" || rbgs.Spec.Template.Roles[1].Name != "prefill" || rbgs.Spec.Template.Roles[2].Name != "decode" {
		t.Fatalf("unexpected role order: %s, %s, %s", rbgs.Spec.Template.Roles[0].Name, rbgs.Spec.Template.Roles[1].Name, rbgs.Spec.Template.Roles[2].Name)
	}
	if len(rbgs.Spec.Template.CoordinationRequirements) == 0 {
		t.Fatal("expected coordination requirements for disaggregated mode")
	}
}

func TestIsArksApplicationReady(t *testing.T) {
	tests := []struct {
		name string
		app  *arksv1.ArksApplication
		want bool
	}{
		{
			name: "unified without router ready",
			app: &arksv1.ArksApplication{
				Status: arksv1.ArksApplicationStatus{
					Unified: arksv1.ArksRoleStatus{Replicas: 2, ReadyReplicas: 2},
				},
			},
			want: true,
		},
		{
			name: "unified with router ready",
			app: &arksv1.ArksApplication{
				Spec: arksv1.ArksApplicationSpec{
					Router: &arksv1.ArksApplicationRouter{},
				},
				Status: arksv1.ArksApplicationStatus{
					Mode:          arksv1.ArksApplicationModeUnified,
					TrafficTarget: arksv1.ArksApplicationTrafficTargetRouter,
					Unified:       arksv1.ArksRoleStatus{Replicas: 1, ReadyReplicas: 1},
					Router:        arksv1.ArksRoleStatus{Replicas: 1, ReadyReplicas: 1},
					Conditions: []arksv1.ArksApplicationCondition{
						{Type: arksv1.ArksApplicationTrafficTargetReady, Status: corev1.ConditionTrue},
					},
				},
			},
			want: true,
		},
		{
			name: "disaggregated ready",
			app: &arksv1.ArksApplication{
				Status: arksv1.ArksApplicationStatus{
					Mode:   arksv1.ArksApplicationModeDisaggregated,
					Router: arksv1.ArksRoleStatus{Replicas: 1, ReadyReplicas: 1},
					Prefill: arksv1.ArksRoleStatus{
						Replicas: 2, ReadyReplicas: 2,
					},
					Decode: arksv1.ArksRoleStatus{
						Replicas: 4, ReadyReplicas: 4,
					},
					Conditions: []arksv1.ArksApplicationCondition{
						{Type: arksv1.ArksApplicationTrafficTargetReady, Status: corev1.ConditionTrue},
					},
				},
			},
			want: true,
		},
		{
			name: "pending traffic target is not ready",
			app: &arksv1.ArksApplication{
				Status: arksv1.ArksApplicationStatus{
					Mode:          arksv1.ArksApplicationModeUnified,
					TrafficTarget: arksv1.ArksApplicationTrafficTargetPending,
					Unified:       arksv1.ArksRoleStatus{Replicas: 1, ReadyReplicas: 1},
				},
			},
			want: false,
		},
		{
			name: "router target not ready",
			app: &arksv1.ArksApplication{
				Spec: arksv1.ArksApplicationSpec{
					Router: &arksv1.ArksApplicationRouter{},
				},
				Status: arksv1.ArksApplicationStatus{
					Mode:    arksv1.ArksApplicationModeUnified,
					Unified: arksv1.ArksRoleStatus{Replicas: 1, ReadyReplicas: 1},
					Router:  arksv1.ArksRoleStatus{Replicas: 1, ReadyReplicas: 0},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isArksApplicationReady(tt.app); got != tt.want {
				t.Fatalf("isArksApplicationReady() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnsureApplicationServiceUsesStableServicePort(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}
	if err := arksv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add arksv1 to scheme: %v", err)
	}

	application := newTestArksApplication()
	application.Name = "test-app"
	application.Namespace = "default"
	application.Spec.Runtime = string(arksv1.ArksRuntimeSGLang)
	application.Spec.Router = &arksv1.ArksApplicationRouter{Port: 31000}

	reconciler := &ArksApplicationReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(application).
			Build(),
		Scheme: scheme,
	}

	if err := reconciler.ensureApplicationService(context.Background(), application, arksv1.ArksApplicationTrafficTargetRouter); err != nil {
		t.Fatalf("ensureApplicationService returned error: %v", err)
	}

	service := &corev1.Service{}
	if err := reconciler.Client.Get(context.Background(), types.NamespacedName{
		Namespace: application.Namespace,
		Name:      generateApplicationServiceName(application),
	}, service); err != nil {
		t.Fatalf("failed to get created service: %v", err)
	}

	if len(service.Spec.Ports) != 1 {
		t.Fatalf("expected 1 service port, got %d", len(service.Spec.Ports))
	}
	if service.Spec.Ports[0].Port != 8080 {
		t.Fatalf("expected stable service port 8080, got %d", service.Spec.Ports[0].Port)
	}
	if service.Spec.Ports[0].TargetPort != intstr.FromInt(31000) {
		t.Fatalf("expected target port 31000, got %+v", service.Spec.Ports[0].TargetPort)
	}
}

func TestSyncApplicationAggregateStatus(t *testing.T) {
	tests := []struct {
		name string
		app  *arksv1.ArksApplication
		want arksv1.ArksApplicationStatus
	}{
		{
			name: "unified aggregates inference only",
			app: &arksv1.ArksApplication{
				Spec: arksv1.ArksApplicationSpec{
					Mode: arksv1.ArksApplicationModeUnified,
				},
				Status: arksv1.ArksApplicationStatus{
					Unified: arksv1.ArksRoleStatus{
						Replicas:        2,
						ReadyReplicas:   2,
						UpdatedReplicas: 1,
					},
					Router: arksv1.ArksRoleStatus{
						Replicas:        1,
						ReadyReplicas:   1,
						UpdatedReplicas: 1,
					},
				},
			},
			want: arksv1.ArksApplicationStatus{
				Replicas:        2,
				ReadyReplicas:   2,
				UpdatedReplicas: 1,
			},
		},
		{
			name: "disaggregated aggregates active roles",
			app: &arksv1.ArksApplication{
				Spec: arksv1.ArksApplicationSpec{
					Mode: arksv1.ArksApplicationModeDisaggregated,
				},
				Status: arksv1.ArksApplicationStatus{
					Router: arksv1.ArksRoleStatus{
						Replicas:        1,
						ReadyReplicas:   1,
						UpdatedReplicas: 1,
					},
					Prefill: arksv1.ArksRoleStatus{
						Replicas:        2,
						ReadyReplicas:   2,
						UpdatedReplicas: 2,
					},
					Decode: arksv1.ArksRoleStatus{
						Replicas:        4,
						ReadyReplicas:   3,
						UpdatedReplicas: 2,
					},
				},
			},
			want: arksv1.ArksApplicationStatus{
				Replicas:        7,
				ReadyReplicas:   6,
				UpdatedReplicas: 5,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syncApplicationAggregateStatus(tt.app)
			if tt.app.Status.Replicas != tt.want.Replicas {
				t.Fatalf("Replicas = %d, want %d", tt.app.Status.Replicas, tt.want.Replicas)
			}
			if tt.app.Status.ReadyReplicas != tt.want.ReadyReplicas {
				t.Fatalf("ReadyReplicas = %d, want %d", tt.app.Status.ReadyReplicas, tt.want.ReadyReplicas)
			}
			if tt.app.Status.UpdatedReplicas != tt.want.UpdatedReplicas {
				t.Fatalf("UpdatedReplicas = %d, want %d", tt.app.Status.UpdatedReplicas, tt.want.UpdatedReplicas)
			}
		})
	}
}

// ---- regression tests for the command override + worker label fixes ----

// TestBuildUnifiedRoleLeaderWorkerOverride covers:
//   - unified.leaderCommandOverride replaces Container.Command for the
//     leader pod
//   - the controller-generated leader command is exposed as
//     ARKS_LEADER_COMMAND env on the leader container
//   - the value of ARKS_LEADER_COMMAND is the RAW command string (not
//     wrapped in /bin/bash -c), so user override scripts can
//     `eval "$ARKS_LEADER_COMMAND"` without double-shell quoting
func TestBuildUnifiedRoleLeaderWorkerOverride(t *testing.T) {
	r := &ArksApplicationReconciler{}
	app := newTestArksApplication()
	app.Spec.Runtime = string(arksv1.ArksRuntimeSGLang)
	app.Spec.Unified.LeaderCommandOverride = []string{"/bin/sh", "-c", "echo custom-leader; eval \"$ARKS_LEADER_COMMAND\""}
	app.Spec.Unified.WorkerCommandOverride = []string{"/bin/sh", "-c", "echo custom-worker"}
	model := newTestArksModel()

	role, err := r.buildUnifiedRole(app, model)
	if err != nil {
		t.Fatalf("buildUnifiedRole returned error: %v", err)
	}

	leaderContainer := role.TemplateSource.Template.Spec.Containers[0]
	if got, want := leaderContainer.Command[0], "/bin/sh"; got != want {
		t.Fatalf("leader Command[0] = %q, want %q (override should replace default)", got, want)
	}
	var leaderEnv string
	for _, e := range leaderContainer.Env {
		if e.Name == "ARKS_LEADER_COMMAND" {
			leaderEnv = e.Value
		}
	}
	if leaderEnv == "" {
		t.Fatal("ARKS_LEADER_COMMAND env var missing when leaderCommandOverride is set")
	}
	if strings.HasPrefix(leaderEnv, "/bin/bash -c ") {
		t.Fatalf("ARKS_LEADER_COMMAND should hold the RAW sglang command, but starts with /bin/bash -c: %q", leaderEnv)
	}
	if !strings.Contains(leaderEnv, "sglang.launch_server") {
		t.Fatalf("ARKS_LEADER_COMMAND should contain the sglang command, got %q", leaderEnv)
	}

	// Worker side: unmarshal the worker patch and assert the override
	// replaces Container.Command + ARKS_WORKER_COMMAND env is the raw
	// sglang worker command (not the /bin/bash -c wrapper).
	var workerPatch corev1.PodTemplateSpec
	if err := json.Unmarshal(role.LeaderWorkerSet.PatchWorkerTemplate.Raw, &workerPatch); err != nil {
		t.Fatalf("worker patch is not a valid PodTemplateSpec: %v", err)
	}
	if len(workerPatch.Spec.Containers) != 1 {
		t.Fatalf("worker patch expects exactly 1 container, got %d", len(workerPatch.Spec.Containers))
	}
	workerContainer := workerPatch.Spec.Containers[0]
	if got, want := workerContainer.Command[0], "/bin/sh"; got != want {
		t.Fatalf("worker Command[0] = %q, want %q (override should replace default)", got, want)
	}
	if !strings.Contains(strings.Join(workerContainer.Command, " "), "custom-worker") {
		t.Fatalf("worker Command should carry the override, got %v", workerContainer.Command)
	}
	var workerEnv string
	for _, e := range workerContainer.Env {
		if e.Name == "ARKS_WORKER_COMMAND" {
			workerEnv = e.Value
		}
	}
	if workerEnv == "" {
		t.Fatal("ARKS_WORKER_COMMAND env var missing when workerCommandOverride is set")
	}
	if strings.HasPrefix(workerEnv, "/bin/bash -c ") {
		t.Fatalf("ARKS_WORKER_COMMAND should hold the RAW sglang command, but starts with /bin/bash -c: %q", workerEnv)
	}
	if !strings.Contains(workerEnv, "sglang.launch_server") {
		t.Fatalf("ARKS_WORKER_COMMAND should contain the sglang command, got %q", workerEnv)
	}
}

// TestBuildUnifiedRoleWorkerPatchLabels covers the size>1 distributed
// inference fix: worker pods must carry arks.ai/role=unified +
// arks.ai/work-load-role=worker, otherwise the unified Service selector
// (arks.ai/work-load-role=leader) would also match them.
func TestBuildUnifiedRoleWorkerPatchLabels(t *testing.T) {
	r := &ArksApplicationReconciler{}
	app := newTestArksApplication()
	app.Spec.Unified.Size = 2 // multi-node LWS group
	model := newTestArksModel()

	role, err := r.buildUnifiedRole(app, model)
	if err != nil {
		t.Fatalf("buildUnifiedRole returned error: %v", err)
	}

	raw := role.LeaderWorkerSet.PatchWorkerTemplate.Raw
	var patch corev1.PodTemplateSpec
	if err := json.Unmarshal(raw, &patch); err != nil {
		t.Fatalf("worker patch is not a valid PodTemplateSpec: %v", err)
	}
	if patch.ObjectMeta.Labels[arksv1.ArksControllerKeyRole] != "unified" {
		t.Fatalf("worker patch arks.ai/role = %q, want unified", patch.ObjectMeta.Labels[arksv1.ArksControllerKeyRole])
	}
	if patch.ObjectMeta.Labels[arksv1.ArksControllerKeyWorkLoadRole] != arksv1.ArksWorkLoadRoleWorker {
		t.Fatalf("worker patch arks.ai/work-load-role = %q, want %q",
			patch.ObjectMeta.Labels[arksv1.ArksControllerKeyWorkLoadRole], arksv1.ArksWorkLoadRoleWorker)
	}
}

// TestBuildRouterRoleARKSRouterCommandEnv covers the router env injection
// fix: when router.commandOverride is set on an sglang application, the
// controller-generated default router command is exposed as
// ARKS_ROUTER_COMMAND to the router container so override scripts can
// compose on top of the original.
func TestBuildRouterRoleARKSRouterCommandEnv(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}
	if err := arksv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add arksv1 to scheme: %v", err)
	}
	app := newTestArksApplication()
	app.Name = "t-router-env"
	app.Namespace = "default"
	app.Spec.Runtime = string(arksv1.ArksRuntimeSGLang)
	app.Spec.Router = &arksv1.ArksApplicationRouter{
		CommandOverride: []string{"/bin/sh", "-c", "echo custom; eval \"$ARKS_ROUTER_COMMAND\""},
		InstanceSpec:    arksv1.ArksInstanceSpec{ServiceAccountName: "router-sa"},
	}

	r := &ArksApplicationReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).Build(),
		Scheme: scheme,
	}
	role, err := r.buildRouterRole(context.Background(), app)
	if err != nil {
		t.Fatalf("buildRouterRole returned error: %v", err)
	}

	container := role.TemplateSource.Template.Spec.Containers[0]
	var routerEnv string
	for _, e := range container.Env {
		if e.Name == "ARKS_ROUTER_COMMAND" {
			routerEnv = e.Value
		}
	}
	if routerEnv == "" {
		t.Fatal("ARKS_ROUTER_COMMAND env var missing when router.commandOverride is set on an sglang app")
	}
	if !strings.Contains(routerEnv, "sglang_router") {
		t.Fatalf("ARKS_ROUTER_COMMAND should reference sglang_router default, got %q", routerEnv)
	}
}

// TestBuildRouterRoleArgsWithCommandOverride covers routerArgs semantics
// when commandOverride is set:
//   - routerArgs is passed to Container.Args (K8s Command+Args convention),
//     so binary-style commandOverride can receive CLI flags via routerArgs
//   - ARKS_ROUTER_COMMAND env is still injected with the default sglang
//     router command for composability
//
// And the default sglang path (no commandOverride) does NOT pass routerArgs
// via Container.Args because generateRouterCommand already appended them
// inside the generated command string.
func TestBuildRouterRoleArgsWithCommandOverride(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}
	if err := arksv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add arksv1 to scheme: %v", err)
	}

	// (a) commandOverride + routerArgs together → Command + Args both set
	appA := newTestArksApplication()
	appA.Name = "t-router-args-a"
	appA.Namespace = "default"
	appA.Spec.Runtime = string(arksv1.ArksRuntimeVLLM)
	appA.Spec.RouterImage = "x"
	appA.Spec.Router = &arksv1.ArksApplicationRouter{
		CommandOverride: []string{"python3", "-m", "my_router"},
		RouterArgs:      []string{"--port", "8080", "--policy", "round_robin"},
		InstanceSpec:    arksv1.ArksInstanceSpec{ServiceAccountName: "router-sa"},
	}
	rA := &ArksApplicationReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(appA).Build(),
		Scheme: scheme,
	}
	roleA, err := rA.buildRouterRole(context.Background(), appA)
	if err != nil {
		t.Fatalf("buildRouterRole (case a) returned error: %v", err)
	}
	cA := roleA.TemplateSource.Template.Spec.Containers[0]
	if got, want := cA.Command, []string{"python3", "-m", "my_router"}; !reflectStringSliceEqual(got, want) {
		t.Fatalf("case (a) Container.Command = %v, want %v", got, want)
	}
	if got, want := cA.Args, []string{"--port", "8080", "--policy", "round_robin"}; !reflectStringSliceEqual(got, want) {
		t.Fatalf("case (a) Container.Args = %v, want %v (routerArgs should be passed as Args)", got, want)
	}

	// (b) sglang default path with routerArgs → Args is NIL because
	//     routerArgs already baked into generated Command via -c "..."
	appB := newTestArksApplication()
	appB.Name = "t-router-args-b"
	appB.Namespace = "default"
	appB.Spec.Runtime = string(arksv1.ArksRuntimeSGLang)
	appB.Spec.Router = &arksv1.ArksApplicationRouter{
		RouterArgs:   []string{"--custom-flag", "yes"},
		InstanceSpec: arksv1.ArksInstanceSpec{ServiceAccountName: "router-sa"},
	}
	rB := &ArksApplicationReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(appB).Build(),
		Scheme: scheme,
	}
	roleB, err := rB.buildRouterRole(context.Background(), appB)
	if err != nil {
		t.Fatalf("buildRouterRole (case b) returned error: %v", err)
	}
	cB := roleB.TemplateSource.Template.Spec.Containers[0]
	if len(cB.Args) != 0 {
		t.Fatalf("case (b) Container.Args = %v, want empty (default sglang path should not double-pass routerArgs)", cB.Args)
	}
	if !strings.Contains(cB.Command[len(cB.Command)-1], "--custom-flag yes") {
		t.Fatalf("case (b) generated Command should contain '--custom-flag yes', got %v", cB.Command)
	}
}

func reflectStringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestValidateDisaggregatedRuntime covers the validate fail-fast for
// non-sglang disaggregated mode plus the ordering fix (disagg runtime
// limit is reported before router-image requirements).
func TestValidateDisaggregatedRuntime(t *testing.T) {
	r := &ArksApplicationReconciler{}

	// disaggregated + vllm should be rejected, and the error must be about
	// the disagg runtime restriction (NOT about routerImage/commandOverride).
	app := &arksv1.ArksApplication{
		Spec: arksv1.ArksApplicationSpec{
			Mode:    arksv1.ArksApplicationModeDisaggregated,
			Runtime: string(arksv1.ArksRuntimeVLLM),
			Model:   corev1.LocalObjectReference{Name: "test-model"},
			Prefill: &arksv1.ArksApplicationWorkload{Replicas: ptrInt32(1), Size: 1},
			Decode:  &arksv1.ArksApplicationWorkload{Replicas: ptrInt32(1), Size: 1},
			Router:  &arksv1.ArksApplicationRouter{Replicas: ptrInt32(1)},
		},
	}
	err := r.validate(app)
	if err == nil {
		t.Fatal("expected validate error for mode=disaggregated runtime=vllm, got nil")
	}
	if !strings.Contains(err.Error(), "disaggregated mode currently only supports runtime=sglang") {
		t.Fatalf("error should explain disagg runtime restriction, got: %v", err)
	}
	if strings.Contains(err.Error(), "spec.routerImage") {
		t.Fatalf("disagg runtime check should fire BEFORE router-image check, got: %v", err)
	}

	// disaggregated + sglang + full required fields should pass.
	app.Spec.Runtime = string(arksv1.ArksRuntimeSGLang)
	if err := r.validate(app); err != nil {
		t.Fatalf("validate should pass for disaggregated+sglang, got: %v", err)
	}
}

// TestValidateUnifiedCustomRouter covers: unified + non-sglang router
// requires both spec.routerImage and spec.router.commandOverride.
func TestValidateUnifiedCustomRouter(t *testing.T) {
	r := &ArksApplicationReconciler{}

	mk := func() *arksv1.ArksApplication {
		return &arksv1.ArksApplication{
			Spec: arksv1.ArksApplicationSpec{
				Mode:    arksv1.ArksApplicationModeUnified,
				Runtime: string(arksv1.ArksRuntimeVLLM),
				Model:   corev1.LocalObjectReference{Name: "test-model"},
				Unified: &arksv1.ArksApplicationWorkload{Replicas: ptrInt32(1), Size: 1},
				Router:  &arksv1.ArksApplicationRouter{Replicas: ptrInt32(1)},
			},
		}
	}

	// missing both routerImage and commandOverride: reject
	app := mk()
	if err := r.validate(app); err == nil {
		t.Fatal("expected validate error for unified+vllm+router without custom router fields")
	} else if !strings.Contains(err.Error(), "router with runtime=vllm requires both spec.routerImage and spec.router.commandOverride") {
		t.Fatalf("error should explain custom router requirement, got: %v", err)
	}

	// only routerImage: still reject (hasCustomRouter requires BOTH)
	app = mk()
	app.Spec.RouterImage = "x"
	if err := r.validate(app); err == nil {
		t.Fatal("expected validate error when only routerImage is set")
	}

	// only commandOverride: still reject (BOTH needed)
	app = mk()
	app.Spec.Router.CommandOverride = []string{"sleep", "1"}
	if err := r.validate(app); err == nil {
		t.Fatal("expected validate error when only commandOverride is set")
	}

	// both set: pass
	app = mk()
	app.Spec.RouterImage = "x"
	app.Spec.Router.CommandOverride = []string{"sleep", "1"}
	if err := r.validate(app); err != nil {
		t.Fatalf("validate should pass for unified+vllm+full custom router, got: %v", err)
	}
}

func newTestArksApplication() *arksv1.ArksApplication {
	return &arksv1.ArksApplication{
		Spec: arksv1.ArksApplicationSpec{
			Runtime: string(arksv1.ArksRuntimeVLLM),
			Model:   corev1.LocalObjectReference{Name: "test-model"},
			Unified: &arksv1.ArksApplicationWorkload{
				Replicas: ptrInt32(2),
				Size:     1,
				RuntimeCommonArgs: []string{
					"--tensor-parallel-size", "1",
				},
			},
		},
	}
}

func newTestArksModel() *arksv1.ArksModel {
	return &arksv1.ArksModel{
		Spec: arksv1.ArksModelSpec{
			Model: "test-model",
			Storage: &arksv1.ArksModelStorage{
				PVC: &arksv1.ArksModelStoragePVC{Name: "test-pvc"},
			},
		},
	}
}

func ptrInt32(v int32) *int32 {
	return &v
}

// testModelsFor wraps the single default model into the resolved-models map
// that generateRBGS expects, keyed by the application's model reference.
func testModelsFor(application *arksv1.ArksApplication, model *arksv1.ArksModel) map[string]*arksv1.ArksModel {
	return map[string]*arksv1.ArksModel{application.Spec.Model.Name: model}
}
