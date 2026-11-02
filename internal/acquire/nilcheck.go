package acquire

import "reflect"

// nilIface は、型付きの nil ポインタを含めて nil かを返す。
func nilIface(v any) bool {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Func, reflect.Interface, reflect.Chan:
		return rv.IsNil()
	}
	return false
}
