package cleanup

import "errors"

var (
	// ErrInvalidServerID is reported when a caller passes an id that is not a
	// node UUID.
	ErrInvalidServerID = errors.New("cleanup: invalid server id")
	// ErrInvalidTrigger is reported for a trigger outside manual, schedule and
	// threshold. Volume removals are refused for every non-manual trigger.
	ErrInvalidTrigger = errors.New("cleanup: invalid trigger")
	// ErrHostUnsupported is reported when a host lacks the tool that owns a
	// root-only category. The scan reports a notice, the clean skips.
	ErrHostUnsupported = errors.New("cleanup: host category unsupported")
	// ErrHostHelperOutdated is reported when the node's host helper predates
	// the cleanup verbs and must be updated on that host.
	ErrHostHelperOutdated = errors.New("cleanup: host helper outdated")
	// ErrHostAgentOutdated is reported when the node agent predates the cleanup
	// RPCs.
	ErrHostAgentOutdated = errors.New("cleanup: node agent outdated")
)
