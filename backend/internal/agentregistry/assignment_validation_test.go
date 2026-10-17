package agentregistry

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func TestValidateAssignmentRejectsNonFiniteScores(t *testing.T) {
	for _, score := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		t.Run(fmt.Sprint(score), func(t *testing.T) {
			assignment := Assignment{
				ID: "assignment-1", OwnerIdentity: "alice@example.com", TaskID: "task-1",
				AgentID: "agent-1", AgentRevision: 1, Score: score,
				RequestDigest: fmt.Sprintf("%064x", 42), AssignedAt: time.Now().UTC(),
			}
			if err := validateAssignment(assignment); err == nil {
				t.Fatalf("assignment with score %v was accepted", score)
			}
		})
	}
}
