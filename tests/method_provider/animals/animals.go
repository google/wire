package animals

type Dog struct{}
type Cat struct{}

type Animals struct{}

func (f *Animals) NewDog() *Dog {
	return &Dog{}
}

func (Animals) NewCat() *Cat {
	return &Cat{}
}

func NewAnimals() *Animals {
	return &Animals{}
}
