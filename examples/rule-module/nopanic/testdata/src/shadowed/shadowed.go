package shadowed

func panic(message string) string {
	return message
}

func Describe() string {
	return panic("not the builtin")
}
