package internal

// IsClosed checks if a channel is closed.
//
// NOTE: It returns true if the channel is closed as well
// as if the channel is not empty. Used internally
// to check if the channel is closed.
func IsClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
	}

	return false
}
