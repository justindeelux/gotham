package cleanup

import "errors"

var (
	// ErrInvalidServerID is reported when a caller passes an id that is not a
	// node UUID.
	ErrInvalidServerID = errors.New("cleanup: invalid server id")
	// ErrInvalidTrigger is reported for a trigger outside manual, schedule and
	// threshold. Volume removals are refused for every non-manual trigger.
	ErrInvalidTrigger = errors.New("cleanup: invalid trigger")
)
