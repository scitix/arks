package controller

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	arksv1 "github.com/arks-ai/arks/api/v1"
)

func newTestDisaggregatedApplication() *arksv1.ArksApplication {
	return &arksv1.ArksApplication{
		ObjectMeta: metav1.ObjectMeta{Name: "t-pd", Namespace: "default"},
		Spec: arksv1.ArksApplicationSpec{
			Mode:    arksv1.ArksApplicationModeDisaggregated,
			Runtime: string(arksv1.ArksRuntimeSGLang),
			Model:   corev1.LocalObjectReference{Name: "test-model"},
			Router: &arksv1.ArksApplicationRouter{
				InstanceSpec: arksv1.ArksInstanceSpec{ServiceAccountName: "router-sa"},
			},
			Decode: &arksv1.ArksApplicationWorkload{
				Replicas:          ptrInt32(1),
				Size:              2,
				RuntimeCommonArgs: []string{"--tp-size", "16"},
			},
		},
	}
}

func newNamedTestArksModel(name, pvc string) *arksv1.ArksModel {
	return &arksv1.ArksModel{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: arksv1.ArksModelSpec{
			Model: name,
			Storage: &arksv1.ArksModelStorage{
				PVC: &arksv1.ArksModelStoragePVC{Name: pvc},
			},
		},
	}
}

// TestGenerateRBGSDisaggregatedPrefillGroups covers the heterogeneous prefill
// feature: every group becomes its own RBG role (prefill-<group>) with its
// own engine args, pod labels, and (optionally) its own model, while decode
// and router are untouched.
func TestGenerateRBGSDisaggregatedPrefillGroups(t *testing.T) {
	reconciler := &ArksApplicationReconciler{}
	application := newTestDisaggregatedApplication()
	application.Spec.PrefillGroups = []arksv1.ArksApplicationWorkloadGroup{
		{
			Name:  "fast",
			Model: &corev1.LocalObjectReference{Name: "sharded-model"},
			ArksApplicationWorkload: arksv1.ArksApplicationWorkload{
				Replicas:          ptrInt32(1),
				Size:              1,
				RuntimeCommonArgs: []string{"--tp", "8", "--dp", "8", "--enable-dp-attention"},
			},
		},
		{
			Name: "long",
			ArksApplicationWorkload: arksv1.ArksApplicationWorkload{
				Replicas:          ptrInt32(1),
				Size:              2,
				RuntimeCommonArgs: []string{"--tp", "16"},
			},
		},
	}

	models := map[string]*arksv1.ArksModel{
		"test-model":    newNamedTestArksModel("test-model", "test-pvc"),
		"sharded-model": newNamedTestArksModel("sharded-model", "sharded-pvc"),
	}

	rbgs, err := reconciler.generateRBGS(context.Background(), application, models)
	if err != nil {
		t.Fatalf("generateRBGS returned error: %v", err)
	}

	roles := rbgs.Spec.Template.Roles
	if len(roles) != 4 {
		t.Fatalf("expected 4 roles (router + 2 prefill groups + decode), got %d", len(roles))
	}
	wantNames := []string{"router", "prefill-fast", "prefill-long", "decode"}
	for i, want := range wantNames {
		if roles[i].Name != want {
			t.Fatalf("role[%d].Name = %q, want %q", i, roles[i].Name, want)
		}
	}

	fast, long, decode := roles[1], roles[2], roles[3]

	// Group sizes map to LWS sizes.
	if got := *fast.LeaderWorkerSet.Size; got != 1 {
		t.Fatalf("prefill-fast LWS size = %d, want 1", got)
	}
	if got := *long.LeaderWorkerSet.Size; got != 2 {
		t.Fatalf("prefill-long LWS size = %d, want 2", got)
	}

	// Pod labels: shared arks.ai/role=prefill (router selector), per-group
	// arks.ai/worker-group, and the model label always carries spec.model.
	fastLabels := fast.TemplateSource.Template.ObjectMeta.Labels
	if fastLabels[arksv1.ArksControllerKeyRole] != "prefill" {
		t.Fatalf("prefill-fast arks.ai/role = %q, want prefill", fastLabels[arksv1.ArksControllerKeyRole])
	}
	if fastLabels[arksv1.ArksControllerKeyWorkerGroup] != "fast" {
		t.Fatalf("prefill-fast arks.ai/worker-group = %q, want fast", fastLabels[arksv1.ArksControllerKeyWorkerGroup])
	}
	if fastLabels[arksv1.ArksControllerKeyModel] != "test-model" {
		t.Fatalf("prefill-fast arks.ai/model = %q, want test-model (spec.model, not the group override)", fastLabels[arksv1.ArksControllerKeyModel])
	}
	var fastLeaderPatch corev1.PodTemplateSpec
	if err := json.Unmarshal(fast.LeaderWorkerSet.PatchLeaderTemplate.Raw, &fastLeaderPatch); err != nil {
		t.Fatalf("prefill-fast leader patch is not a valid PodTemplateSpec: %v", err)
	}
	if fastLeaderPatch.ObjectMeta.Labels[arksv1.ArksControllerKeyWorkerGroup] != "fast" {
		t.Fatalf("prefill-fast leader patch arks.ai/worker-group = %q, want fast", fastLeaderPatch.ObjectMeta.Labels[arksv1.ArksControllerKeyWorkerGroup])
	}
	decodeLabels := decode.TemplateSource.Template.ObjectMeta.Labels
	if decodeLabels[arksv1.ArksControllerKeyRole] != "decode" {
		t.Fatalf("decode arks.ai/role = %q, want decode", decodeLabels[arksv1.ArksControllerKeyRole])
	}
	if _, ok := decodeLabels[arksv1.ArksControllerKeyWorkerGroup]; ok {
		t.Fatal("decode pods must not carry the arks.ai/worker-group label")
	}

	// Per-group engine commands.
	fastCmd := strings.Join(fast.TemplateSource.Template.Spec.Containers[0].Command, " ")
	if !strings.Contains(fastCmd, "--tp 8 --dp 8 --enable-dp-attention") {
		t.Fatalf("prefill-fast command should carry the group's runtimeCommonArgs, got %q", fastCmd)
	}
	if !strings.Contains(fastCmd, "--disaggregation-mode prefill") {
		t.Fatalf("prefill-fast command should run in prefill disaggregation mode, got %q", fastCmd)
	}
	longCmd := strings.Join(long.TemplateSource.Template.Spec.Containers[0].Command, " ")
	if !strings.Contains(longCmd, "--tp 16") || strings.Contains(longCmd, "--enable-dp-attention") {
		t.Fatalf("prefill-long command should carry only its own runtimeCommonArgs, got %q", longCmd)
	}

	// Per-group model: command path and PVC volume follow the override; the
	// group without an override falls back to spec.model.
	if !strings.Contains(fastCmd, "--model-path /models/models/default/sharded-model") {
		t.Fatalf("prefill-fast command should use the overridden model path, got %q", fastCmd)
	}
	if !strings.Contains(longCmd, "--model-path /models/models/default/test-model") {
		t.Fatalf("prefill-long command should fall back to spec.model path, got %q", longCmd)
	}
	if got := fast.TemplateSource.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName; got != "sharded-pvc" {
		t.Fatalf("prefill-fast model volume PVC = %q, want sharded-pvc", got)
	}
	if got := long.TemplateSource.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName; got != "test-pvc" {
		t.Fatalf("prefill-long model volume PVC = %q, want test-pvc", got)
	}
	if got := decode.TemplateSource.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName; got != "test-pvc" {
		t.Fatalf("decode model volume PVC = %q, want test-pvc", got)
	}
}

// TestGenerateRBGSDisaggregatedLegacyPrefillRoleName pins the upgrade
// guarantee: a single spec.prefill keeps the role name "prefill" (and thus
// the underlying LWS name), so existing deployments are not recreated.
func TestGenerateRBGSDisaggregatedLegacyPrefillRoleName(t *testing.T) {
	reconciler := &ArksApplicationReconciler{}
	application := newTestDisaggregatedApplication()
	application.Spec.Prefill = &arksv1.ArksApplicationWorkload{
		Replicas:          ptrInt32(2),
		Size:              1,
		RuntimeCommonArgs: []string{"--tp", "8"},
	}

	models := map[string]*arksv1.ArksModel{"test-model": newNamedTestArksModel("test-model", "test-pvc")}
	rbgs, err := reconciler.generateRBGS(context.Background(), application, models)
	if err != nil {
		t.Fatalf("generateRBGS returned error: %v", err)
	}

	roles := rbgs.Spec.Template.Roles
	if len(roles) != 3 {
		t.Fatalf("expected 3 roles, got %d", len(roles))
	}
	if roles[1].Name != "prefill" {
		t.Fatalf("legacy single prefill must keep role name \"prefill\", got %q", roles[1].Name)
	}
	labels := roles[1].TemplateSource.Template.ObjectMeta.Labels
	if _, ok := labels[arksv1.ArksControllerKeyWorkerGroup]; ok {
		t.Fatal("legacy single prefill pods must not carry the arks.ai/worker-group label")
	}
}

// TestValidatePrefillGroupsExclusive mirrors the CRD CEL rule in the
// controller: disaggregated mode requires exactly one of prefill /
// prefillGroups.
func TestValidatePrefillGroupsExclusive(t *testing.T) {
	r := &ArksApplicationReconciler{}

	group := arksv1.ArksApplicationWorkloadGroup{
		Name:                    "fast",
		ArksApplicationWorkload: arksv1.ArksApplicationWorkload{Replicas: ptrInt32(1), Size: 1},
	}

	// neither prefill nor prefillGroups: reject
	app := newTestDisaggregatedApplication()
	if err := r.validate(app); err == nil {
		t.Fatal("expected validate error when neither prefill nor prefillGroups is set")
	}

	// both: reject
	app = newTestDisaggregatedApplication()
	app.Spec.Prefill = &arksv1.ArksApplicationWorkload{Replicas: ptrInt32(1), Size: 1}
	app.Spec.PrefillGroups = []arksv1.ArksApplicationWorkloadGroup{group}
	if err := r.validate(app); err == nil {
		t.Fatal("expected validate error when both prefill and prefillGroups are set")
	}

	// groups only: pass
	app = newTestDisaggregatedApplication()
	app.Spec.PrefillGroups = []arksv1.ArksApplicationWorkloadGroup{group}
	if err := r.validate(app); err != nil {
		t.Fatalf("validate should pass with prefillGroups only, got: %v", err)
	}

	// legacy prefill only: pass
	app = newTestDisaggregatedApplication()
	app.Spec.Prefill = &arksv1.ArksApplicationWorkload{Replicas: ptrInt32(1), Size: 1}
	if err := r.validate(app); err != nil {
		t.Fatalf("validate should pass with prefill only, got: %v", err)
	}

	// duplicate group names: reject even though the CRD's listMapKey would
	// normally block this at admission (defense in depth for programmatic
	// paths).
	app = newTestDisaggregatedApplication()
	app.Spec.PrefillGroups = []arksv1.ArksApplicationWorkloadGroup{group, group}
	if err := r.validate(app); err == nil {
		t.Fatal("expected validate error for duplicate prefill group names")
	} else if !strings.Contains(err.Error(), "duplicate prefill group name") {
		t.Fatalf("error should mention duplicate group name, got: %v", err)
	}
}
