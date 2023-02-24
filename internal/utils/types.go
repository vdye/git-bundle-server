package utils

type Pair[T any, R any] struct {
	First  T
	Second R
}

func NewPair[T any, R any](first T, second R) Pair[T, R] {
	return Pair[T, R]{
		First:  first,
		Second: second,
	}
}
