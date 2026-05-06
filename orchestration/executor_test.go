package orchestration_test

import (
	"errors"
	"testing"

	"github.com/Germanblandin1/goagent/orchestration"
)

func TestStageContext_RequireOutput_found(t *testing.T) {
	sc := orchestration.NewStageContext("test goal")
	sc.SetOutput("research", "research results")

	got, err := sc.RequireOutput("research")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "research results" {
		t.Errorf("got %q, want %q", got, "research results")
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
	sc.SetArtifact("score", "not a float")

	_, err := orchestration.GetArtifact[float64](sc, "score")

	if err == nil {
		t.Fatal("expected type mismatch error, got nil")
	}
}

func TestNewStrictStageContext_noCollision_nilErrors(t *testing.T) {
	sc := orchestration.NewStrictStageContext("goal")
	sc.SetOutput("a", "v1")
	sc.SetArtifact("score", 0.9)

	if cols := sc.CollisionErrors(); cols != nil {
		t.Errorf("expected nil collisions, got: %v", cols)
	}
}

func TestNewStrictStageContext_outputCollision_recordsError(t *testing.T) {
	sc := orchestration.NewStrictStageContext("goal")
	sc.SetOutput("result", "first")
	sc.SetOutput("result", "second")

	cols := sc.CollisionErrors()
	if len(cols) != 1 {
		t.Fatalf("expected 1 collision, got %d: %v", len(cols), cols)
	}
	var colErr *orchestration.KeyCollisionError
	if !errors.As(cols[0], &colErr) {
		t.Fatalf("expected *KeyCollisionError, got %T", cols[0])
	}
	if colErr.Key != "result" {
		t.Errorf("Key: got %q, want %q", colErr.Key, "result")
	}
	if colErr.Namespace != "output" {
		t.Errorf("Namespace: got %q, want %q", colErr.Namespace, "output")
	}
}

func TestNewStrictStageContext_artifactCollision_recordsError(t *testing.T) {
	sc := orchestration.NewStrictStageContext("goal")
	sc.SetArtifact("score", 0.8)
	sc.SetArtifact("score", 0.9)

	cols := sc.CollisionErrors()
	if len(cols) != 1 {
		t.Fatalf("expected 1 collision, got %d: %v", len(cols), cols)
	}
	var colErr *orchestration.KeyCollisionError
	if !errors.As(cols[0], &colErr) {
		t.Fatalf("expected *KeyCollisionError, got %T", cols[0])
	}
	if colErr.Namespace != "artifact" {
		t.Errorf("Namespace: got %q, want %q", colErr.Namespace, "artifact")
	}
}

func TestNewStrictStageContext_multipleCollisions_allRecorded(t *testing.T) {
	sc := orchestration.NewStrictStageContext("goal")
	sc.SetOutput("x", "1")
	sc.SetOutput("x", "2") // collision
	sc.SetOutput("y", "3")
	sc.SetOutput("y", "4") // collision

	cols := sc.CollisionErrors()
	if len(cols) != 2 {
		t.Errorf("expected 2 collisions, got %d: %v", len(cols), cols)
	}
}

func TestNewStageContext_nonStrict_noCollisionTracking(t *testing.T) {
	sc := orchestration.NewStageContext("goal")
	sc.SetOutput("result", "first")
	sc.SetOutput("result", "second") // silent overwrite

	if cols := sc.CollisionErrors(); cols != nil {
		t.Errorf("non-strict StageContext must never record collisions, got: %v", cols)
	}
	if v, _ := sc.Output("result"); v != "second" {
		t.Errorf("last write must win in non-strict mode, got %q", v)
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
