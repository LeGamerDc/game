package calc

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParse(t *testing.T) {
	var e error

	_, e = parse("(x+y)*z")
	assert.Nil(t, e)

	_, e = parse("x >= y + 3")
	assert.Nil(t, e)

	_, e = parse("5 + (-x)")
	assert.Nil(t, e)

	_, e = parse("x % 7 <= 2")
	assert.Nil(t, e)

	_, e = parse("x < 5 || y > 3")
	assert.Nil(t, e)

	_, e = parse("x < 5 ? y+2 : z+3")
	assert.Nil(t, e)

	_, e = parse("x = y*21")
	assert.Nil(t, e)

	_, e = parse("int x")
	assert.Nil(t, e)

	_, e = parse("float y,z")
	assert.Nil(t, e)

	_, e = parse("bool x,z; float h; int p, q")
	assert.Nil(t, e)

	_, e = parse("int x,y,z; z = (x+3)*(y+2); z % 2 == 0")
	fmt.Println(e)
	assert.Nil(t, e)

	_, e = parse("x, y = y, x")
	assert.NotNil(t, e)

	_, e = parse("x & y")
	assert.NotNil(t, e)

	_, e = parse("pp x,y,z")
	assert.NotNil(t, e)

	_, e = parse("x !=< y")
	assert.NotNil(t, e)
}
