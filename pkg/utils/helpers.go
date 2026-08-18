package utils

func Map[T, V any](in []T, fn func(T) V) []V {
	out := make([]V, len(in))
	for i, t := range in {
		out[i] = fn(t)
	}
	return out
}
