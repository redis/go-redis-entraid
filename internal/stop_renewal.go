package internal

// StopRenewal ends subscription-driven scheduling without retiring acquisition
// that a foreground reader requested. Owner shutdown retires that work too.
type StopRenewal func() error
