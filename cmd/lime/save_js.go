package main

import (
	"fmt"
	"strconv"
	"syscall/js"

	"github.com/egoist/mygo"
)

func init() {
	askSaveConflict = func(_ *app, o mygo.MessageOptions) (mygo.MessageResult, error) {
		message := o.Message + "\n" + o.Detail
		for i, label := range o.Buttons {
			message += fmt.Sprintf("\n%d. %s", i+1, label)
		}
		choice := js.Global().Call("prompt", message, strconv.Itoa(o.CancelButton+1))
		button := o.CancelButton
		if choice.Type() == js.TypeString {
			n, err := strconv.Atoi(choice.String())
			if err == nil && n > 0 && n <= len(o.Buttons) {
				button = n - 1
			}
		}
		return mygo.MessageResult{Button: button}, nil
	}
}
