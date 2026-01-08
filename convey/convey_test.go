package convey

import (
	"testing"
)

func TestTest(t *testing.T) {
	Convey("something", t, func() {
		So(123, ShouldEqual, 123)

		Convey("A", func() {
			So(123, ShouldEqual, 124)
		})
		Convey("B", func() {

		})
	})
}
