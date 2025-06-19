package main

import (
	"fmt"
	"github.com/legamerdc/game/blackboard"

	"github.com/legamerdc/game/calc"
)

var test = `
float power, power_x, power_y;

power = power_x * 0.95 + power_y * 1.25;

power > 3000
`

type Kv struct {
	m map[string]blackboard.Field
}

func (k *Kv) Get(s string) (blackboard.Field, bool) {
	v, ok := k.m[s]
	return v, ok
}

func (k *Kv) SetInt64(s string, i int64) {
	k.m[s] = blackboard.Int64(i)
}

func (k *Kv) SetFloat64(s string, f float64) {
	k.m[s] = blackboard.Float64(f)
}

func (k *Kv) SetBool(s string, b bool) {
	k.m[s] = blackboard.Bool(b)
}

func main() {
	kv := Kv{m: make(map[string]blackboard.Field)}
	kv.m["power_x"] = blackboard.Int64(3000)
	kv.m["power_y"] = blackboard.Int64(3000)
	f, e := calc.Compile[*Kv](test)
	if e != nil {
		fmt.Println("compile: ", e)
		return
	}
	v, e1 := f(&kv)
	if e1 != nil {
		fmt.Println("run: ", e1)
		return
	}
	fmt.Println(v.Bool())
	fmt.Println(kv.m["power"].Float64())
}
