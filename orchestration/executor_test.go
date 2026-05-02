package orchestration_test

import (
	"testing"

	"github.com/Germanblandin1/goagent/orchestration"
)

func TestStageContext_RequireOutput_found(t *testing.T) {
	sc := orchestration.NewStageContext("test goal")
	sc.SetOutput("research", "resultado de investigación")

	got, err := sc.RequireOutput("research")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "resultado de investigación" {
		t.Errorf("got %q, want %q", got, "resultado de investigación")
	}
}

func TestStageContext_RequireOutput_notFound(t *testing.T) {
	sc := orchestration.NewStageContext("test goal")

	_, err := sc.RequireOutput("missing")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestStageContext_Output_found(t *testing.T) {
	sc := orchestration.NewStageContext("test goal")
	sc.SetOutput("key", "value")

	got, ok := sc.Output("key")

	if !ok {
		t.Fatal("expected ok=true, got false")
	}
	if got != "value" {
		t.Errorf("got %q, want %q", got, "value")
	}
}

func TestStageContext_Output_notFound(t *testing.T) {
	sc := orchestration.NewStageContext("test goal")

	_, ok := sc.Output("missing")

	if ok {
		t.Fatal("expected ok=false, got true")
	}
}

func TestStageContext_Outputs_snapshot(t *testing.T) {
	sc := orchestration.NewStageContext("test goal")
	sc.SetOutput("a", "va")
	sc.SetOutput("b", "vb")

	snap := sc.Outputs()

	if snap["a"] != "va" || snap["b"] != "vb" {
		t.Errorf("unexpected snapshot: %v", snap)
	}

	// mutating the snapshot must not affect the StageContext
	snap["a"] = "mutated"
	if v, _ := sc.Output("a"); v != "va" {
		t.Error("snapshot mutation leaked into StageContext")
	}
}

func TestGetArtifact_typed(t *testing.T) {
	sc := orchestration.NewStageContext("test goal")
	sc.SetArtifact("deps", []string{"pgx/v5", "chi"})

	got, err := orchestration.GetArtifact[[]string](sc, "deps")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "pgx/v5" {
		t.Errorf("unexpected value: %v", got)
	}
}

func TestGetArtifact_notFound(t *testing.T) {
	sc := orchestration.NewStageContext("test goal")

	_, err := orchestration.GetArtifact[float64](sc, "missing")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetArtifact_wrongType(t *testing.T) {
	sc := orchestration.NewStageContext("test goal")
	sc.SetArtifact("score", "no soy un float")

	_, err := orchestration.GetArtifact[float64](sc, "score")

	if err == nil {
		t.Fatal("expected type mismatch error, got nil")
	}
}

func TestStageContext_Artifacts_snapshot(t *testing.T) {
	sc := orchestration.NewStageContext("test goal")
	sc.SetArtifact("x", 42)

	snap := sc.Artifacts()

	if snap["x"] != 42 {
		t.Errorf("unexpected snapshot: %v", snap)
	}

	// mutating the snapshot must not affect the StageContext
	snap["x"] = 99
	if v, _ := sc.Artifact("x"); v != 42 {
		t.Error("snapshot mutation leaked into StageContext")
	}
}
