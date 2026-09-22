// Package cgoformat reports gofmt drift against this file, not cgo's rewrite.
package cgoformat // want "file is not gofmt-formatted"

import "C"

func Three() int {
    return  3
}
