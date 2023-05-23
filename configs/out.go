/*
	Copyright (C) CESS. All rights reserved.
	Copyright (C) Cumulus Encrypted Storage System. All rights reserved.

	SPDX-License-Identifier: Apache-2.0
*/

package configs

import "fmt"

const (
	HiBlack = iota + 90
	HiRed
	HiGreen
	HiYellow
	HiBlue
	HiPurple
	HiCyan
	HiWhite
)

const (
	OkPrompt    = "OK"
	WarnPrompt  = "!!"
	ErrPrompt   = "XX"
	InputPrompt = ">>"
	TipPrompt   = "++"
)

func Input(msg string) {
	fmt.Println(textInput(), msg)
}

func Tip(msg string) {
	fmt.Println(textTip(), msg)
}

func Err(msg string) {
	fmt.Println(textErr(), msg)
}

func Warn(msg string) {
	fmt.Println(textWarn(), msg)
}

func Ok(msg string) {
	fmt.Println(textOk(), msg)
}

func textTip() string {
	return fmt.Sprintf("\x1b[0;%dm%s\x1b[0m", HiPurple, TipPrompt)
}

func textInput() string {
	return fmt.Sprintf("\x1b[0;%dm%s\x1b[0m", HiBlue, InputPrompt)
}

func textErr() string {
	return fmt.Sprintf("\x1b[0;%dm%s\x1b[0m", HiRed, ErrPrompt)
}

func textOk() string {
	return fmt.Sprintf("\x1b[0;%dm%s\x1b[0m", HiGreen, OkPrompt)
}

func textWarn() string {
	return fmt.Sprintf("\x1b[0;%dm%s\x1b[0m", HiYellow, WarnPrompt)
}