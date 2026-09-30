//go:build !darwin && !nogui

package desktop

func reduceMotion() bool { return false }
