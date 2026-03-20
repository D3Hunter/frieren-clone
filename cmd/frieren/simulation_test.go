package main

import "testing"

func TestSimulationModeEnabled(t *testing.T) {
	t.Setenv(simulationModeEnv, "1")
	if !simulationModeEnabled() {
		t.Fatalf("expected simulation mode enabled when %s=1", simulationModeEnv)
	}

	t.Setenv(simulationModeEnv, "false")
	if simulationModeEnabled() {
		t.Fatalf("expected simulation mode disabled when %s=false", simulationModeEnv)
	}
}

func TestSimulationUseRealMCP(t *testing.T) {
	t.Setenv(simulationRealMCPEnv, "true")
	if !simulationUseRealMCP() {
		t.Fatalf("expected real mcp enabled when %s=true", simulationRealMCPEnv)
	}

	t.Setenv(simulationRealMCPEnv, "0")
	if simulationUseRealMCP() {
		t.Fatalf("expected real mcp disabled when %s=0", simulationRealMCPEnv)
	}
}

func TestSimulationRounds(t *testing.T) {
	t.Setenv(simulationRoundsEnv, "")
	value, err := simulationRounds()
	if err != nil {
		t.Fatalf("unexpected error for empty rounds: %v", err)
	}
	if value != defaultSimulationRounds {
		t.Fatalf("expected default rounds %d, got %d", defaultSimulationRounds, value)
	}

	t.Setenv(simulationRoundsEnv, "5")
	value, err = simulationRounds()
	if err != nil {
		t.Fatalf("unexpected error for rounds=5: %v", err)
	}
	if value != 5 {
		t.Fatalf("expected rounds=5, got %d", value)
	}

	t.Setenv(simulationRoundsEnv, "nope")
	if _, err := simulationRounds(); err == nil {
		t.Fatalf("expected parse error for non-numeric rounds")
	}
}

func TestIsSimulationFailureReply(t *testing.T) {
	if !isSimulationFailureReply("Execution failed: timeout\nDiagnostic ID: req_123") {
		t.Fatalf("expected execution failure reply to be detected")
	}
	if isSimulationFailureReply("Execution failed: timeout") {
		t.Fatalf("expected missing diagnostic id to be ignored")
	}
	if isSimulationFailureReply("normal markdown response") {
		t.Fatalf("expected normal response not treated as failure")
	}
}
