/*******************************************************************************
 * Copyright (c) 2025 Genome Research Ltd.
 *
 * Author: Michael Woolnough <mw31@sanger.ac.uk>
 *
 * Permission is hereby granted, free of charge, to any person obtaining
 * a copy of this software and associated documentation files (the
 * "Software"), to deal in the Software without restriction, including
 * without limitation the rights to use, copy, modify, merge, publish,
 * distribute, sublicense, and/or sell copies of the Software, and to
 * permit persons to whom the Software is furnished to do so, subject to
 * the following conditions:
 *
 * The above copyright notice and this permission notice shall be included
 * in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
 * EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
 * MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
 * IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
 * CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
 * TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
 * SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 ******************************************************************************/

package convey

import (
	"cmp"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"unsafe"
)

var (
	stack = make(map[conveyID]*conveyStack)
	files = make(map[string]int)
)

type conveyID [2]int

type conveyStack struct {
	t          *testing.T
	stack      []*conveyFrame
	stackPos   int
	assertions uint64

	resetFns []func()
}

func (c *conveyStack) nextTest() {
	c.cleanup()

	for _, f := range c.stack {
		f.first = false
		f.num = 0
	}

	for len(c.stack) > 0 {
		last := c.stack[len(c.stack)-1]

		last.skip++

		if last.skip < last.total {
			break
		}

		c.stack = c.stack[:len(c.stack)-1]
	}
}

func (c *conveyStack) cleanup() {
	for _, fn := range slices.Backward(c.resetFns) {
		fn()
	}

	c.resetFns = c.resetFns[:0]
}

type conveyFrame struct {
	first bool
	skip  int
	num   int
	total int
}

func Convey(msg string, args ...any) {
	id, init := getStackID()

	if init {
		initConvey(msg, id, args)
	} else {
		runConvey(msg, id, args)
	}
}

func getStackID() (conveyID, bool) {
	fn := Convey
	fnPtr := **(**uintptr)(unsafe.Pointer(&fn))
	lastID := conveyID{-1, -1}
	lastFile := ""
	count := 0
	next := false

	for i := 1; ; i++ {
		ptr, file, id, ok := runtime.Caller(i)
		if !ok {
			break
		}

		if next {
			next = false
			lastID[0] = id
			lastFile = file
		}

		if runtime.FuncForPC(ptr).Entry() == fnPtr {
			count++
			next = true
		}
	}

	if lastID[0] == -1 {
		panic("Not init'd")
	}

	fileID, ok := files[lastFile]
	if !ok {
		fileID = len(files)
		files[lastFile] = fileID
	}

	lastID[1] = fileID

	return lastID, count == 1
}

func initConvey(msg string, id conveyID, args []any) {
	if len(args) != 2 {
		panic("require *testing.T and func() as second and third arguments")
	}

	t, ok := args[0].(*testing.T)
	if !ok {
		panic("need *testing.T as second argument")
	}

	_, ok = args[1].(func())
	if !ok {
		panic("require func() as third argument")
	}

	frame := &conveyStack{t: t, stack: []*conveyFrame{{first: true}}}
	stack[id] = frame

	defer delete(stack, id)

	t.Cleanup(frame.cleanup)

	for len(frame.stack) > 0 {
		runConvey(msg, id, args[1:])

		frame.nextTest()
	}

	if testing.Verbose() {
		fmt.Println()
	}

	fmt.Printf("\n\033[32m%d total assertions\033[0m\n\n", frame.assertions)

	delete(stack, id)
}

func runConvey(msg string, id conveyID, args []any) {
	if len(args) != 1 {
		panic("require func() as second argument")
	}

	fn, ok := args[0].(func())
	if !ok {
		panic("require func() as second argument")
	}

	s := stack[id]
	frame := s.stack[s.stackPos]

	if frame.first {
		frame.total++
	}

	if frame.skip == frame.num {
		s.stackPos++

		if len(s.stack) <= s.stackPos {
			s.stack = append(s.stack, &conveyFrame{first: true})
		}

		if s.stack[s.stackPos].first && testing.Verbose() {
			fmt.Printf("\n%s%s: ", strings.Repeat(" ", 2*s.stackPos), msg)
		}

		fn()

		s.stackPos--
	}

	frame.num++
}

func So[T any](v T, m func(T, ...T) error, cs ...T) {
	id, _ := getStackID()
	s := stack[id]
	t := s.t

	s.assertions++

	t.Helper()

	if err := m(v, cs...); err != nil {
		if testing.Verbose() {
			fmt.Print("\033[33m✘\033[0m")
		} else {
			fmt.Print("\033[33mx\033[0m")
		}

		t.Fatal(err)

	}

	if testing.Verbose() {
		fmt.Print("\033[32m✔\033[0m")
	} else {
		fmt.Print("\033[32m.\033[0m")
	}
}

func ShouldBeNil[T any](v T, _ ...T) error {
	switch v := reflect.ValueOf(v); v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		if v.IsNil() {
			return nil
		}
	case reflect.Invalid:
		return nil
	}

	return fmt.Errorf("expecting %v to be <nil>, but it wasn't", v)
}

func ShouldNotBeNil[T any](v T, _ ...T) error {
	switch v := reflect.ValueOf(v); v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		if !v.IsNil() {
			return nil
		}
	}

	return fmt.Errorf("expecting %v to not be <nil>, but it was", v)
}

func ShouldBeTrue[T bool](v T, _ ...T) error {
	if v {
		return nil
	}

	return fmt.Errorf("expecting %v to not be true, but it wasn't", v)
}

func ShouldBeFalse[T bool](v T, _ ...T) error {
	if !v {
		return nil
	}

	return fmt.Errorf("expecting %v to not be false, but it wasn't", v)
}

func ShouldEqual[T comparable](v T, cs ...T) error {
	if v == cs[0] {
		return nil
	}

	return fmt.Errorf("expecting %v to equal %v, but it didn't", v, cs[0])
}

func ShouldNotEqual[T comparable](v T, cs ...T) error {
	if v != cs[0] {
		return nil
	}

	return fmt.Errorf("expecting %v to not equal %v, but it did", v, cs[0])
}

func ShouldBeGreaterThan[T cmp.Ordered](v T, cs ...T) error {
	if v > cs[0] {
		return nil
	}

	return fmt.Errorf("expecting %v to be greater-than %v, but it wasn't", v, cs[0])
}

func ShouldResemble[T any](v T, cs ...T) error {
	if reflect.DeepEqual(v, cs[0]) {
		return nil
	}

	return fmt.Errorf("expecting %v to be resemble %v, but it didn't", v, cs[0])
}

func Reset(fn func()) {
	id, _ := getStackID()

	var ran atomic.Bool

	cfn := func() {
		if !ran.Swap(true) {
			fn()
		}
	}

	s := stack[id]
	s.resetFns = append(stack[id].resetFns, cfn)
	s.t.Cleanup(cfn)
}
