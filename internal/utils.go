package internal

// IsClosed checks if a channel is closed.
// Returns true only if the channel is actually closed, not just if it has data available.
//
// WARNING: This function will consume one value from the channel if it has pending data.
// Use with caution on channels where consuming data might cause issues.
func IsClosed(ch <-chan struct{}) bool {
	select {
	case _, ok := <-ch:
		// If ok is false, the channel is closed
		// If ok is true, the channel had data (which we just consumed)
		return !ok
	default:
		// Channel is open but has no data available
		return false
	}
}
