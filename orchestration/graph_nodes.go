package orchestration

import "context"

// ExecutorNode converts an Executor into a NodeFunc with a fixed next node.
// Use when a node always transitions to the same next node regardless of
// its output.
//
// Example:
//
//	orchestration.WithNode("review", orchestration.ExecutorNode(reviewAdapter, "synthesize"))
//	orchestration.WithNode("final",  orchestration.ExecutorNode(finalAdapter, ""))  // "" = END
func ExecutorNode(executor Executor, next string) NodeFunc {
	return func(ctx context.Context, sc *StageContext) (string, error) {
		return next, executor.RunWithContext(ctx, sc)
	}
}

// ApprovalRequest carries the information shown to the human for approval.
type ApprovalRequest struct {
	// NodeName is the name of the node that requested approval.
	NodeName string
	// Outputs is a snapshot of sc.Outputs at the time of the request.
	Outputs map[string]string
	// Artifacts is a snapshot of sc.Artifacts at the time of the request.
	Artifacts map[string]any
}

// ApprovalResponse carries the human's decision.
type ApprovalResponse struct {
	// Approved indicates whether the human approved the current state.
	Approved bool
	// Reason is an optional explanation, used when Approved is false.
	// The graph stores it as artifact "rejection_reason" for subsequent nodes.
	Reason string
}

// HumanApprovalNode creates a NodeFunc that pauses the graph and waits for
// human approval before continuing.
//
// When reached, the node sends an ApprovalRequest to requestCh and blocks
// until it receives an ApprovalResponse from responseCh or the context
// is cancelled.
//
// The caller is responsible for reading from requestCh, presenting the
// information to the user, and sending the decision to responseCh.
// The graph should run in a separate goroutine so the caller can handle
// the channels concurrently.
//
// onApproved is the next node if the human approves.
// onRejected is the next node if the human rejects. The rejection reason
// is stored in sc.Artifacts["rejection_reason"] for subsequent nodes.
//
// Example:
//
//	requestCh  := make(chan orchestration.ApprovalRequest)
//	responseCh := make(chan orchestration.ApprovalResponse)
//
//	graph, _ := orchestration.NewGraph(
//	    orchestration.WithStart("generate"),
//	    orchestration.WithNode("generate", orchestration.ExecutorNode(coderAdapter, "approve")),
//	    orchestration.WithNode("approve",  orchestration.HumanApprovalNode(
//	        requestCh, responseCh, "execute", "generate",
//	    )),
//	    orchestration.WithNode("execute", orchestration.ExecutorNode(runnerAdapter, "")),
//	)
//
//	go func() {
//	    sc, err = graph.Run(ctx, goal)
//	    close(requestCh)
//	}()
//
//	for req := range requestCh {
//	    fmt.Printf("Approve? outputs: %v\n", req.Outputs)
//	    var input string
//	    fmt.Scan(&input)
//	    responseCh <- orchestration.ApprovalResponse{Approved: input == "y"}
//	}
func HumanApprovalNode(
	requestCh chan<- ApprovalRequest,
	responseCh <-chan ApprovalResponse,
	onApproved string,
	onRejected string,
) NodeFunc {
	return func(ctx context.Context, sc *StageContext) (string, error) {
		req := ApprovalRequest{
			Outputs:   sc.Outputs(),
			Artifacts: sc.Artifacts(),
		}

		select {
		case requestCh <- req:
		case <-ctx.Done():
			return "", ctx.Err()
		}

		select {
		case resp := <-responseCh:
			if resp.Approved {
				return onApproved, nil
			}
			sc.SetArtifact("rejection_reason", resp.Reason)
			return onRejected, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}
