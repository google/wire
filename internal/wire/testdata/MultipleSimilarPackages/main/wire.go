// Copyright 2018 The Wire Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//+build wireinject

package main

import (
	"fmt"

	"example.com/bar"
	"example.com/baz"
	"example.com/foo"
	"github.com/google/wire"
)

type MainConfig struct {
	Foo *foo.Config
	Bar *bar.Config
	Baz *baz.Config
}

type MainService struct {
	Foo *foo.Service
	Bar *bar.Service
	Baz *baz.Service
}

func (m *MainService) String() string {
	return fmt.Sprintf("%d %d %d", m.Foo.Cfg.V, m.Bar.Cfg.V, m.Baz.Cfg.V)
}

func newMainService(*foo.Config, *bar.Config, *baz.Config) *MainService {
	wire.Build(
		MainService{},
		foo.New,
		bar.New,
		baz.New,
	)
	return nil
}

func main() {
	cfg := &MainConfig{
		Foo: &foo.Config{1},
		Bar: &bar.Config{2},
		Baz: &baz.Config{3},
	}
	svc := newMainService(cfg.Foo, cfg.Bar, cfg.Baz)
	fmt.Println(svc.String())
}
