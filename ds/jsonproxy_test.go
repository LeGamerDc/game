package ds

import (
	"fmt"
	"testing"

	jsoniter "github.com/json-iterator/go"
	"github.com/stretchr/testify/assert"
)

type (
	mix interface {
		Id() int32
		OK()
	}

	cd struct {
		ID   int32  `json:"id"`
		Text string `json:"text"`
	}

	cb struct {
		ID    int32  `json:"id"`
		Name  string `json:"name"`
		Cd    int64  `json:"cd"`
		Count int    `json:"count"`
	}

	ca struct {
		X   int        `json:"x"`
		B   Proxy[*cb] `json:"b"`
		D   Proxy[*cd] `json:"d"`
		Mix Proxy[mix] `json:"mix"`
	}
)

func (b *cb) Id() int32 {
	return b.ID
}

func (b *cb) OK() {}

func (d *cd) Id() int32 {
	return d.ID
}

func (d *cd) OK() {}

func TestProxy(t *testing.T) {
	var (
		e error
		a ca
		b cb
		d cd
	)
	e = jsoniter.Unmarshal([]byte(`{"id":1,"name":"test","cd":7000,"count":3}`), &b)
	assert.Nil(t, e)
	RegisterProxy[*cb](&b)

	e = jsoniter.Unmarshal([]byte(`{"id":3, "text":"hello, world"}`), &d)
	assert.Nil(t, e)
	RegisterProxy[*cd](&d)

	RegisterProxy[mix](&b)
	RegisterProxy[mix](&d)
	e = jsoniter.Unmarshal([]byte(`{"x":13,"b":1,"d":3,"mix":3}`), &a)
	assert.Nil(t, e)

	assert.Equal(t, b, *a.B.Get())
	assert.Equal(t, d, *a.D.Get())
	assert.Equal(t, mix(&d), a.Mix.Get())

	fmt.Println(*a.B.Get())
	fmt.Println(*a.D.Get())
	fmt.Println(a.Mix.Get())
}
