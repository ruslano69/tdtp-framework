package schema

// appendDFSFrame grows a bounded depth-first traversal stack without allowing
// a graph shape to create unbounded retained memory.
func appendDFSFrame[T any](stack []T, frame T, limit int) []T {
	if len(stack) < cap(stack) {
		return append(stack, frame)
	}
	if len(stack) >= limit {
		panic("DFS stack exceeds graph size")
	}
	newCapacity := limit
	if cap(stack) <= limit/2 {
		newCapacity = max(1, cap(stack)*2)
	}
	grown := make([]T, len(stack), newCapacity)
	copy(grown, stack)
	return append(grown, frame)
}
