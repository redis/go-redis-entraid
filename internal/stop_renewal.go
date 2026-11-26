package internal

// StopRenewal ends subscription-driven scheduling without retiring acquisition
// that a foreground reader requested. Owner shutdown retires that work too.
// It lives in internal so only this module's manager can implement the
// provider's subscriptionManager interface; custom managers keep
// Start/StopFunc.
type StopRenewal func() error
