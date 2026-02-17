package handlers

import "github.com/4xmen/payambar/pkg/i18n"

func __(message string) string {
	return i18n.Translate(message)
}
