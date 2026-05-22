package trial

import "errors"

// ErrTrialAlreadyUsed is returned by Manager.Activate when a TrialRecord
// already exists (regardless of whether it's expired). One trial per install.
var ErrTrialAlreadyUsed = errors.New("trial already used")
