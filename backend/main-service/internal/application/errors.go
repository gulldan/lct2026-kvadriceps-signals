package application

import "errors"

// ErrForbidden HTTP-слой превращает в 404, чтобы не раскрывать существование чужих ресурсов.
var ErrForbidden = errors.New("forbidden")

// ErrInvalidArgument уходит клиенту как 400, а не как внутренняя ошибка.
var ErrInvalidArgument = errors.New("invalid argument")

var ErrNotFound = errors.New("not found")
