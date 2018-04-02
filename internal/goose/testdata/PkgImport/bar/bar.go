package bar

type Bar int

//goose:provide Bar
func ProvideBar() Bar {
	return 1
}
