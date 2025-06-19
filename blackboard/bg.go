package blackboard

type Background interface {
	GetInt32(string) (int32, bool)
	GetInt64(string) (int64, bool)
	GetFloat32(string) (float32, bool)
	GetFloat64(string) (float64, bool)
	GetBool(string) (bool, bool)
	GetString(string) (string, bool)

	SetInt32(string, int32)
	SetInt64(string, int64)
	SetFloat32(string, float32)
	SetFloat64(string, float64)
	SetBool(string, bool)
	SetString(string, string)

	GetAny(string) (interface{}, bool)
	SetAny(string, interface{})
}
