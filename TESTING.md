## Behavioral testing

A behavioral test describes what the system does at its seams. The contract is the subject.

When I write a behavioral test, I run it in isolation with a coverage tool and confirm that the lines executed by the test are the lines of the code under test. Coverage of incidental paths is something else; what I want is coverage of the stated subject.

Then I spawn a cold subagent for an alignment check. The cold subagent receives three things: the code under test, the description of what the test is testing, and the test code itself. The cold subagent verifies that all three describe the same behavior, and that the test checks the returned value's data structure deeply — every field that matters, the way the data is shaped, the relationships between fields. Surface assertions (`err == nil`, `result != nil`) are a starting point; the test goes further. The subagent is cold because the implementer carries assumptions that hide gaps; a fresh perspective sees them.

The test is complete when the cold check returns clean. Until then, the test is in progress.

## The writing cycle

A code change is complete when the writing cycle has run.

The writing cycle for any Go change:

1. `gofmt` — formatting holds.
2. `go vet ./...` — static analysis is clean.
3. `go test ./...` — the test suite passes.
4. `go test -race ./...` — the race detector finds nothing.

I run each of these after the change. The cycle is part of writing — a change that has been through the cycle is complete; a change that has not is in progress.
