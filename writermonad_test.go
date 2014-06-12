package tiff

import (
	"errors"
	"testing"
)

type errorIn struct {
	Err error
	N   int
}

func (e *errorIn) decrement() {
	e.N--
}
func (e *errorIn) Write(b []byte) (int, error) {
	defer e.decrement()
	if e.N == 0 {
		return 0, e.Err
	}
	return len(b), nil
}

func TestMonad(t *testing.T) {
	e := errors.New("test error")
	monad := &writerMonad{W: &errorIn{e, 2}}
	for i := 0; i < 5; i++ {
		n, err := monad.Write([]byte("testinput"))
		if i < 2 {
			if got, want := n, 9; got != want {
				t.Errorf("got %v, want %v", got, want)
			}
			if err != nil {
				t.Error(err)
			}
		} else {
			if got, want := n, 0; got != want {
				t.Errorf("got %v, want %v", got, want)
			}
			if err == nil {
				t.Errorf("expecting err")
			}
		}
	}
}
