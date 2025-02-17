// Copyright 2020 Zhizhesihai (Beijing) Technology Limited.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package zetta

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	"cloud.google.com/go/civil"
	"github.com/golang/protobuf/proto"
	"github.com/sunxiaoguang/zetta-client-go/internal/fields"
	tspb "github.com/zhihu/zetta-proto/pkg/tablestore"
	"google.golang.org/grpc/codes"
)

//
// 新增的多种 NULL 类型
//
// NullInt64 represents a Cloud Spanner INT64 that may be NULL.
type NullInt64 struct {
	Int64 int64
	Valid bool // Valid is true if Int64 is not NULL.
}

// String implements Stringer.String for NullInt64
func (n NullInt64) String() string {
	if !n.Valid {
		return fmt.Sprintf("%v", "<null>")
	}
	return fmt.Sprintf("%v", n.Int64)
}

// NullString represents a Cloud Spanner STRING that may be NULL.
type NullString struct {
	StringVal string
	Valid     bool // Valid is true if StringVal is not NULL.
}

// String implements Stringer.String for NullString
func (n NullString) String() string {
	if !n.Valid {
		return fmt.Sprintf("%v", "<null>")
	}
	return fmt.Sprintf("%q", n.StringVal)
}

// NullFloat64 represents a Cloud Spanner FLOAT64 that may be NULL.
type NullFloat64 struct {
	Float64 float64
	Valid   bool // Valid is true if Float64 is not NULL.
}

// String implements Stringer.String for NullFloat64
func (n NullFloat64) String() string {
	if !n.Valid {
		return fmt.Sprintf("%v", "<null>")
	}
	return fmt.Sprintf("%v", n.Float64)
}

// NullBool represents a Cloud Spanner BOOL that may be NULL.
type NullBool struct {
	Bool  bool
	Valid bool // Valid is true if Bool is not NULL.
}

// String implements Stringer.String for NullBool
func (n NullBool) String() string {
	if !n.Valid {
		return fmt.Sprintf("%v", "<null>")
	}
	return fmt.Sprintf("%v", n.Bool)
}

// NullTime represents a Cloud Spanner TIMESTAMP that may be null.
type NullTime struct {
	Time  time.Time
	Valid bool // Valid is true if Time is not NULL.
}

// String implements Stringer.String for NullTime
func (n NullTime) String() string {
	if !n.Valid {
		return fmt.Sprintf("%s", "<null>")
	}
	return fmt.Sprintf("%q", n.Time.Format(time.RFC3339Nano))
}

// NullDate represents a Cloud Spanner DATE that may be null.
type NullDate struct {
	Date  civil.Date
	Valid bool // Valid is true if Date is not NULL.
}

// String implements Stringer.String for NullDate
func (n NullDate) String() string {
	if !n.Valid {
		return fmt.Sprintf("%s", "<null>")
	}
	return fmt.Sprintf("%q", n.Date)
}

// NullRow represents a Cloud Spanner STRUCT that may be NULL.
// See also the document for Row.
// Note that NullRow is not a valid Cloud Spanner column Type.
type NullRow struct {
	Row   Row
	Valid bool // Valid is true if Row is not NULL.
}

// 通用的字段类型和字段值，用于查询结果类型不可知的查询
// GenericColumnValue represents the generic encoded value and type of the
// column.  See google.spanner.v1.ResultSet proto for details.  This can be
// useful for proxying query results when the result types are not known in
// advance.
type GenericColumnValue struct {
	Type  *tspb.Type
	Value *tspb.Value
}

// 从通用 cv 中解码到 ptr 中
// Decode decodes a GenericColumnValue. The ptr argument should be a pointer
// to a Go value that can accept v.
func (v GenericColumnValue) Decode(ptr interface{}) error {
	return decodeValue(v.Value, v.Type, ptr)
}

// NewGenericColumnValue creates a GenericColumnValue from Go value that is
// valid for Cloud Spanner.
func NewGenericColumnValue(v interface{}) (*GenericColumnValue, error) {
	value, typ, err := encodeValue(v)
	if err != nil {
		return nil, err
	}
	return &GenericColumnValue{Value: value, Type: typ}, nil
}

// errTypeMismatch returns error for destination not having a compatible type
// with source Cloud Spanner type.
func errTypeMismatch(srcType tspb.TypeCode, isArray bool, dst interface{}) error {
	usage := srcType.String()
	if isArray {
		usage = fmt.Sprintf("%v[%v]", tspb.TypeCode_ARRAY, srcType)
	}
	return wrapError(codes.InvalidArgument, "type %T cannot be used for decoding %v", dst, usage)
}

// errNilSpannerType returns error for nil Cloud Spanner type in decoding.
func errNilSpannerType() error {
	return wrapError(codes.FailedPrecondition, "unexpected nil Cloud Spanner data type in decoding")
}

// errNilSrc returns error for decoding from nil proto value.
func errNilSrc() error {
	return wrapError(codes.FailedPrecondition, "unexpected nil Cloud Spanner value in decoding")
}

// errNilDst returns error for decoding into nil interface{}.
func errNilDst(dst interface{}) error {
	return wrapError(codes.InvalidArgument, "cannot decode into nil type %T", dst)
}

// errNilArrElemType returns error for input Cloud Spanner data type being a array but without a
// non-nil array element type.
func errNilArrElemType(t *tspb.Type) error {
	return wrapError(codes.FailedPrecondition, "array type %v is with nil array element type", t)
}

// errDstNotForNull returns error for decoding a SQL NULL value into a destination which doesn't
// support NULL values.
func errDstNotForNull(dst interface{}) error {
	return wrapError(codes.InvalidArgument, "destination %T cannot support NULL SQL values", dst)
}

// errBadEncoding returns error for decoding wrongly encoded BYTES/INT64.
func errBadEncoding(v *tspb.Value, err error) error {
	return wrapError(codes.FailedPrecondition, "%v wasn't correctly encoded: <%v>", v, err)
}

func parseNullTime(v *tspb.Value, p *NullTime, code tspb.TypeCode, isNull bool) error {
	if p == nil {
		return errNilDst(p)
	}
	if code != tspb.TypeCode_TIMESTAMP {
		return errTypeMismatch(code, false, p)
	}
	if isNull {
		*p = NullTime{}
		return nil
	}
	x, err := getStringValue(v)
	if err != nil {
		return err
	}
	y, err := time.Parse(time.RFC3339Nano, x)
	if err != nil {
		return errBadEncoding(v, err)
	}
	p.Valid = true
	p.Time = y
	return nil
}

//
// 将 protobuf 值编码到 Go tspb.Type 类型的指针中
//
// decodeValue decodes a protobuf Value into a pointer to a Go value, as specified by tspb.Type.
func decodeValue(v *tspb.Value, t *tspb.Type, ptr interface{}) error {
	if v == nil {
		return errNilSrc()
	}
	if t == nil {
		return errNilSpannerType()
	}
	code := t.Code

	if t.Code == tspb.TypeCode_TYPE_CODE_UNSPECIFIED {
		return decodeSparseValue(v, t, ptr)
	}
	acode := tspb.TypeCode_TYPE_CODE_UNSPECIFIED
	if code == tspb.TypeCode_ARRAY {
		if t.ArrayElementType == nil {
			return errNilArrElemType(t)
		}
		acode = t.ArrayElementType.Code
	}
	typeErr := errTypeMismatch(code, false, ptr)
	if code == tspb.TypeCode_ARRAY {
		typeErr = errTypeMismatch(acode, true, ptr)
	}
	nullErr := errDstNotForNull(ptr)
	_, isNull := v.Kind.(*tspb.Value_NullValue)

	// Do the decoding based on the type of ptr.
	switch p := ptr.(type) {
	case nil:
		return errNilDst(nil)
	case *string:
		if p == nil {
			return errNilDst(p)
		}
		if code != tspb.TypeCode_STRING {
			return typeErr
		}
		if isNull {
			return nullErr
		}
		x, err := getStringValue(v)
		if err != nil {
			return err
		}
		*p = x
	case *NullString:
		if p == nil {
			return errNilDst(p)
		}
		if code != tspb.TypeCode_STRING {
			return typeErr
		}
		if isNull {
			*p = NullString{}
			break
		}
		x, err := getStringValue(v)
		if err != nil {
			return err
		}
		p.Valid = true
		p.StringVal = x
	case *[]NullString:
		if p == nil {
			return errNilDst(p)
		}
		if acode != tspb.TypeCode_STRING {
			return typeErr
		}
		if isNull {
			*p = nil
			break
		}
		x, err := getListValue(v)
		if err != nil {
			return err
		}
		y, err := decodeStringArray(x)
		if err != nil {
			return err
		}
		*p = y
	case *[]byte:
		if p == nil {
			return errNilDst(p)
		}
		if code != tspb.TypeCode_BYTES {
			return typeErr
		}
		if isNull {
			*p = nil
			break
		}
		x, err := getBytesValue(v)
		if err != nil {
			return err
		}
		*p = x
	case *[][]byte:
		if p == nil {
			return errNilDst(p)
		}
		if acode != tspb.TypeCode_BYTES {
			return typeErr
		}
		if isNull {
			*p = nil
			break
		}
		x, err := getListValue(v)
		if err != nil {
			return err
		}
		y, err := decodeByteArray(x)
		if err != nil {
			return err
		}
		*p = y
	case *int64:
		if p == nil {
			return errNilDst(p)
		}
		if code != tspb.TypeCode_INT64 {
			return typeErr
		}
		if isNull {
			return nullErr
		}

		x, err := getInteger64Value(v)
		if err != nil {
			return err
		}

		*p = x
	case *NullInt64:
		if p == nil {
			return errNilDst(p)
		}
		if code != tspb.TypeCode_INT64 {
			return typeErr
		}
		if isNull {
			*p = NullInt64{}
			break
		}
		x, err := getInteger64Value(v)
		if err != nil {
			return err
		}

		p.Valid = true
		p.Int64 = x
	case *[]NullInt64:
		if p == nil {
			return errNilDst(p)
		}
		if acode != tspb.TypeCode_INT64 {
			return typeErr
		}
		if isNull {
			*p = nil
			break
		}
		x, err := getListValue(v)
		if err != nil {
			return err
		}
		y, err := decodeIntArray(x)
		if err != nil {
			return err
		}
		*p = y
	case *bool:
		if p == nil {
			return errNilDst(p)
		}
		if code != tspb.TypeCode_BOOL {
			return typeErr
		}
		if isNull {
			return nullErr
		}
		x, err := getBoolValue(v)
		if err != nil {
			return err
		}
		*p = x
	case *NullBool:
		if p == nil {
			return errNilDst(p)
		}
		if code != tspb.TypeCode_BOOL {
			return typeErr
		}
		if isNull {
			*p = NullBool{}
			break
		}
		x, err := getBoolValue(v)
		if err != nil {
			return err
		}
		p.Valid = true
		p.Bool = x
	case *[]NullBool:
		if p == nil {
			return errNilDst(p)
		}
		if acode != tspb.TypeCode_BOOL {
			return typeErr
		}
		if isNull {
			*p = nil
			break
		}
		x, err := getListValue(v)
		if err != nil {
			return err
		}
		y, err := decodeBoolArray(x)
		if err != nil {
			return err
		}
		*p = y
	case *float64:
		if p == nil {
			return errNilDst(p)
		}
		if code != tspb.TypeCode_FLOAT64 {
			return typeErr
		}
		if isNull {
			return nullErr
		}
		x, err := getFloat64Value(v)
		if err != nil {
			return err
		}
		*p = x
	case *NullFloat64:
		if p == nil {
			return errNilDst(p)
		}
		if code != tspb.TypeCode_FLOAT64 {
			return typeErr
		}
		if isNull {
			*p = NullFloat64{}
			break
		}
		x, err := getFloat64Value(v)
		if err != nil {
			return err
		}
		p.Valid = true
		p.Float64 = x
	case *[]NullFloat64:
		if p == nil {
			return errNilDst(p)
		}
		if acode != tspb.TypeCode_FLOAT64 {
			return typeErr
		}
		if isNull {
			*p = nil
			break
		}
		x, err := getListValue(v)
		if err != nil {
			return err
		}
		y, err := decodeFloat64Array(x)
		if err != nil {
			return err
		}
		*p = y
	case *time.Time:
		var nt NullTime
		if isNull {
			return nullErr
		}
		err := parseNullTime(v, &nt, code, isNull)
		if err != nil {
			return nil
		}
		*p = nt.Time
	case *NullTime:
		err := parseNullTime(v, p, code, isNull)
		if err != nil {
			return err
		}
	case *[]NullTime:
		if p == nil {
			return errNilDst(p)
		}
		if acode != tspb.TypeCode_TIMESTAMP {
			return typeErr
		}
		if isNull {
			*p = nil
			break
		}
		x, err := getListValue(v)
		if err != nil {
			return err
		}
		y, err := decodeTimeArray(x)
		if err != nil {
			return err
		}
		*p = y
	case *civil.Date:
		if p == nil {
			return errNilDst(p)
		}
		if code != tspb.TypeCode_DATE {
			return typeErr
		}
		if isNull {
			return nullErr
		}
		x, err := getStringValue(v)
		if err != nil {
			return err
		}
		y, err := civil.ParseDate(x)
		if err != nil {
			return errBadEncoding(v, err)
		}
		*p = y
	case *NullDate:
		if p == nil {
			return errNilDst(p)
		}
		if code != tspb.TypeCode_DATE {
			return typeErr
		}
		if isNull {
			*p = NullDate{}
			break
		}
		x, err := getStringValue(v)
		if err != nil {
			return err
		}
		y, err := civil.ParseDate(x)
		if err != nil {
			return errBadEncoding(v, err)
		}
		p.Valid = true
		p.Date = y
	case *[]NullDate:
		if p == nil {
			return errNilDst(p)
		}
		if acode != tspb.TypeCode_DATE {
			return typeErr
		}
		if isNull {
			*p = nil
			break
		}
		x, err := getListValue(v)
		if err != nil {
			return err
		}
		y, err := decodeDateArray(x)
		if err != nil {
			return err
		}
		*p = y
	case *[]NullRow:
		if p == nil {
			return errNilDst(p)
		}
		if acode != tspb.TypeCode_STRUCT {
			return typeErr
		}
		if isNull {
			*p = nil
			break
		}
		x, err := getListValue(v)
		if err != nil {
			return err
		}
		y, err := decodeRowArray(t.ArrayElementType.StructType, x)
		if err != nil {
			return err
		}
		*p = y
	case *GenericColumnValue:
		*p = GenericColumnValue{
			// Deep clone to ensure subsequent changes to t or v
			// don't affect our decoded value.
			Type:  proto.Clone(t).(*tspb.Type),
			Value: proto.Clone(v).(*tspb.Value),
		}
	default:
		// Check if the proto encoding is for an array of structs.
		if !(code == tspb.TypeCode_ARRAY && acode == tspb.TypeCode_STRUCT) {
			return typeErr
		}
		vp := reflect.ValueOf(p)
		if !vp.IsValid() {
			return errNilDst(p)
		}
		if !isPtrStructPtrSlice(vp.Type()) {
			// The container is not a pointer to a struct pointer slice.
			return typeErr
		}
		// Only use reflection for nil detection on slow path.
		// Also, IsNil panics on many types, so check it after the type check.
		if vp.IsNil() {
			return errNilDst(p)
		}
		if isNull {
			// The proto Value is encoding NULL, set the pointer to struct
			// slice to nil as well.
			vp.Elem().Set(reflect.Zero(vp.Elem().Type()))
			break
		}
		x, err := getListValue(v)
		if err != nil {
			return err
		}
		if err = decodeStructArray(t.ArrayElementType.StructType, x, p); err != nil {
			return err
		}
	}
	return nil
}

// errSrvVal returns an error for getting a wrong source protobuf value in decoding.
func errSrcVal(v *tspb.Value, want string) error {
	return wrapError(codes.FailedPrecondition, "cannot use %v(Kind: %T) as Value_%sValue in decoding",
		v, v.GetKind(), want)
}

// getStringValue returns the string value encoded in tspb.Value v whose
// kind is tspb.Value_StringValue.
func getStringValue(v *tspb.Value) (string, error) {
	if x, ok := v.GetKind().(*tspb.Value_StringValue); ok && x != nil {
		return x.StringValue, nil
	}
	return "", errSrcVal(v, "String")
}

// getBoolValue returns the bool value encoded in tspb.Value v whose
// kind is tspb.Value_BoolValue.
func getBoolValue(v *tspb.Value) (bool, error) {
	if x, ok := v.GetKind().(*tspb.Value_BoolValue); ok && x != nil {
		return x.BoolValue, nil
	}
	return false, errSrcVal(v, "Bool")
}

// getListValue returns the tspb.ListValue contained in tspb.Value v whose
// kind is tspb.Value_ListValue.
func getListValue(v *tspb.Value) (*tspb.ListValue, error) {
	if x, ok := v.GetKind().(*tspb.Value_ListValue); ok && x != nil {
		return x.ListValue, nil
	}
	return nil, errSrcVal(v, "List")
}

// errUnexpectedNumStr returns error for decoder getting a unexpected string for
// representing special float values.
func errUnexpectedNumStr(s string) error {
	return wrapError(codes.FailedPrecondition, "unexpected string value %q for number", s)
}

// getFloat64Value returns the float64 value encoded in tspb.Value v whose
// kind is tspb.Value_NumberValue / tspb.Value_StringValue.
// Cloud Spanner uses string to encode NaN, Infinity and -Infinity.
func getFloat64Value(v *tspb.Value) (float64, error) {
	switch x := v.GetKind().(type) {
	case *tspb.Value_NumberValue:
		if x == nil {
			break
		}
		return x.NumberValue, nil
	case *tspb.Value_StringValue:
		if x == nil {
			break
		}
		switch x.StringValue {
		case "NaN":
			return math.NaN(), nil
		case "Infinity":
			return math.Inf(1), nil
		case "-Infinity":
			return math.Inf(-1), nil
		default:
			return 0, errUnexpectedNumStr(x.StringValue)
		}
	}
	return 0, errSrcVal(v, "Number")
}

// getInteger64Value returns the int64 value encoded in tspb.Value v whose
// kind is tspb.Value_IntegerValue
func getInteger64Value(v *tspb.Value) (int64, error) {
	if x, ok := v.GetKind().(*tspb.Value_IntegerValue); ok && x != nil {
		return x.IntegerValue, nil
	}
	return 0, errSrcVal(v, "Integer")
}

// getTimestampValue returns the timestamp value encoded in tspb.Value v whose
// kind is tspb.Value_TimestampValue
func getTimestampValue(v *tspb.Value) (time.Time, error) {
	if x, ok := v.GetKind().(*tspb.Value_TimestampValue); ok && x != nil {
		tsv := x.TimestampValue
		return time.Unix(tsv.Seconds, int64(tsv.Nanos)), nil
	}
	return time.Time{}, errSrcVal(v, "Timestamp")
}

// getDateValue returns the date value encoded in tspb.Value v whose
// kind is tspb.Value_TimestampValue
func getDateValue(v *tspb.Value) (civil.Date, error) {
	if x, ok := v.GetKind().(*tspb.Value_TimestampValue); ok && x != nil {
		tsv := x.TimestampValue
		date := civil.DateOf(time.Unix(tsv.Seconds, int64(tsv.Nanos)))
		return date, nil
	}
	return civil.Date{}, errSrcVal(v, "Date")
}

// getStringValue returns the string value encoded in tspb.Value v whose
// kind is tspb.Value_StringValue.
func getBytesValue(v *tspb.Value) ([]byte, error) {
	if x, ok := v.GetKind().(*tspb.Value_BytesValue); ok && x != nil {
		return x.BytesValue, nil
	}
	return nil, errSrcVal(v, "Bytes")
}

// errNilListValue returns error for unexpected nil ListValue in decoding Cloud Spanner ARRAYs.
func errNilListValue(sqlType string) error {
	return wrapError(codes.FailedPrecondition, "unexpected nil ListValue in decoding %v array", sqlType)
}

// errDecodeArrayElement returns error for failure in decoding single array element.
func errDecodeArrayElement(i int, v proto.Message, sqlType string, err error) error {
	se, ok := err.(*Error)
	if !ok {
		return wrapError(codes.Unknown,
			"cannot decode %v(array element %v) as %v, error = <%v>", v, i, sqlType, err)
	}
	se.decorate(fmt.Sprintf("cannot decode %v(array element %v) as %v", v, i, sqlType))
	return se
}

// decodeStringArray decodes tspb.ListValue pb into a NullString slice.
func decodeStringArray(pb *tspb.ListValue) ([]NullString, error) {
	if pb == nil {
		return nil, errNilListValue("STRING")
	}
	a := make([]NullString, len(pb.Values))
	for i, v := range pb.Values {
		if err := decodeValue(v, stringType(), &a[i]); err != nil {
			return nil, errDecodeArrayElement(i, v, "STRING", err)
		}
	}
	return a, nil
}

// decodeIntArray decodes tspb.ListValue pb into a NullInt64 slice.
func decodeIntArray(pb *tspb.ListValue) ([]NullInt64, error) {
	if pb == nil {
		return nil, errNilListValue("INT64")
	}
	a := make([]NullInt64, len(pb.Values))
	for i, v := range pb.Values {
		if err := decodeValue(v, intType(), &a[i]); err != nil {
			return nil, errDecodeArrayElement(i, v, "INT64", err)
		}
	}
	return a, nil
}

// decodeBoolArray decodes tspb.ListValue pb into a NullBool slice.
func decodeBoolArray(pb *tspb.ListValue) ([]NullBool, error) {
	if pb == nil {
		return nil, errNilListValue("BOOL")
	}
	a := make([]NullBool, len(pb.Values))
	for i, v := range pb.Values {
		if err := decodeValue(v, boolType(), &a[i]); err != nil {
			return nil, errDecodeArrayElement(i, v, "BOOL", err)
		}
	}
	return a, nil
}

// decodeFloat64Array decodes tspb.ListValue pb into a NullFloat64 slice.
func decodeFloat64Array(pb *tspb.ListValue) ([]NullFloat64, error) {
	if pb == nil {
		return nil, errNilListValue("FLOAT64")
	}
	a := make([]NullFloat64, len(pb.Values))
	for i, v := range pb.Values {
		if err := decodeValue(v, floatType(), &a[i]); err != nil {
			return nil, errDecodeArrayElement(i, v, "FLOAT64", err)
		}
	}
	return a, nil
}

// decodeByteArray decodes tspb.ListValue pb into a slice of byte slice.
func decodeByteArray(pb *tspb.ListValue) ([][]byte, error) {
	if pb == nil {
		return nil, errNilListValue("BYTES")
	}
	a := make([][]byte, len(pb.Values))
	for i, v := range pb.Values {
		if err := decodeValue(v, bytesType(), &a[i]); err != nil {
			return nil, errDecodeArrayElement(i, v, "BYTES", err)
		}
	}
	return a, nil
}

// decodeTimeArray decodes tspb.ListValue pb into a NullTime slice.
func decodeTimeArray(pb *tspb.ListValue) ([]NullTime, error) {
	if pb == nil {
		return nil, errNilListValue("TIMESTAMP")
	}
	a := make([]NullTime, len(pb.Values))
	for i, v := range pb.Values {
		if err := decodeValue(v, timeType(), &a[i]); err != nil {
			return nil, errDecodeArrayElement(i, v, "TIMESTAMP", err)
		}
	}
	return a, nil
}

// decodeDateArray decodes tspb.ListValue pb into a NullDate slice.
func decodeDateArray(pb *tspb.ListValue) ([]NullDate, error) {
	if pb == nil {
		return nil, errNilListValue("DATE")
	}
	a := make([]NullDate, len(pb.Values))
	for i, v := range pb.Values {
		if err := decodeValue(v, dateType(), &a[i]); err != nil {
			return nil, errDecodeArrayElement(i, v, "DATE", err)
		}
	}
	return a, nil
}

func errNotStructElement(i int, v *tspb.Value) error {
	return errDecodeArrayElement(i, v, "STRUCT",
		wrapError(codes.FailedPrecondition, "%v(type: %T) doesn't encode Cloud Spanner STRUCT", v, v))
}

// decodeRowArray decodes tspb.ListValue pb into a NullRow slice according to
// the structual information given in tspb.StructType ty.
func decodeRowArray(ty *tspb.StructType, pb *tspb.ListValue) ([]NullRow, error) {
	if pb == nil {
		return nil, errNilListValue("STRUCT")
	}
	a := make([]NullRow, len(pb.Values))
	for i := range pb.Values {
		switch v := pb.Values[i].GetKind().(type) {
		case *tspb.Value_ListValue:
			a[i] = NullRow{
				Row: Row{
					fields: ty.Fields,
					vals:   v.ListValue.Values,
				},
				Valid: true,
			}
		// Null elements not currently supported by the server, see
		// https://cloud.google.com/spanner/docs/query-syntax#using-structs-with-select
		case *tspb.Value_NullValue:
			// no-op, a[i] is NullRow{} already
		default:
			return nil, errNotStructElement(i, pb.Values[i])
		}
	}
	return a, nil
}

// structFieldColumn returns the name of i-th field of struct type typ if the field
// is untagged; otherwise, it returns the tagged name of the field.
func structFieldColumn(typ reflect.Type, i int) (col string, ok bool) {
	desc := typ.Field(i)
	if desc.PkgPath != "" || desc.Anonymous {
		// Skip unexported or anonymous fields.
		return "", false
	}
	col = desc.Name
	if tag := desc.Tag.Get("spanner"); tag != "" {
		if tag == "-" {
			// Skip fields tagged "-" to match encoding/json and others.
			return "", false
		}
		col = tag
		if idx := strings.Index(tag, ","); idx != -1 {
			col = tag[:idx]
		}
	}
	return col, true
}

// errNilSpannerStructType returns error for unexpected nil Cloud Spanner STRUCT schema type in decoding.
func errNilSpannerStructType() error {
	return wrapError(codes.FailedPrecondition, "unexpected nil StructType in decoding Cloud Spanner STRUCT")
}

// errUnnamedField returns error for decoding a Cloud Spanner STRUCT with unnamed field into a Go struct.
func errUnnamedField(ty *tspb.StructType, i int) error {
	return wrapError(codes.InvalidArgument, "unnamed field %v in Cloud Spanner STRUCT %+v", i, ty)
}

func errUnnamedCellField(cell *tspb.Cell, i int) error {
	return wrapError(codes.InvalidArgument, "unnamed field %v in Zetta Cell %+v", i, cell)
}

// errNoOrDupGoField returns error for decoding a Cloud Spanner
// STRUCT into a Go struct which is either missing a field, or has duplicate fields.
func errNoOrDupGoField(s interface{}, f string) error {
	return wrapError(codes.InvalidArgument, "Go struct %+v(type %T) has no or duplicate fields for Cloud Spanner STRUCT field %v", s, s, f)
}

// errDupColNames returns error for duplicated Cloud Spanner STRUCT field names found in decoding a Cloud Spanner STRUCT into a Go struct.
func errDupSpannerField(f string, ty *tspb.StructType) error {
	return wrapError(codes.InvalidArgument, "duplicated field name %q in Cloud Spanner STRUCT %+v", f, ty)
}

func errDupCellField(f string, ty *tspb.Cell) error {
	return wrapError(codes.InvalidArgument, "duplicated field name %q in Zetta Cell %+v", f, ty)
}

// errDecodeStructField returns error for failure in decoding a single field of a Cloud Spanner STRUCT.
func errDecodeStructField(ty *tspb.StructType, f string, err error) error {
	se, ok := err.(*Error)
	if !ok {
		return wrapError(codes.Unknown,
			"cannot decode field %v of Cloud Spanner STRUCT %+v, error = <%v>", f, ty, err)
	}
	se.decorate(fmt.Sprintf("cannot decode field %v of Cloud Spanner STRUCT %+v", f, ty))
	return se
}

func errDecodeCellField(ty *tspb.Cell, f string, err error) error {
	se, ok := err.(*Error)
	if !ok {
		return wrapError(codes.Unknown,
			"cannot decode field %v of Zetta Cell %+v, error = <%v>", f, ty, err)
	}
	se.decorate(fmt.Sprintf("cannot decode field %v of Zetta Cell %+v", f, ty))
	return se
}

// decodeStruct decodes tspb.ListValue pb into struct referenced by pointer ptr, according to
// the structual information given in tspb.StructType ty.
func decodeStruct(ty *tspb.StructType, pb *tspb.ListValue, ptr interface{}) error {
	if reflect.ValueOf(ptr).IsNil() {
		return errNilDst(ptr)
	}
	if ty == nil {
		return errNilSpannerStructType()
	}
	// t holds the structual information of ptr.
	t := reflect.TypeOf(ptr).Elem()
	// v is the actual value that ptr points to.
	v := reflect.ValueOf(ptr).Elem()

	fields, err := fieldCache.Fields(t)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for i, f := range ty.Fields {
		if f.Name == "" {
			return errUnnamedField(ty, i)
		}
		sf := fields.Match(f.Name)
		if sf == nil {

			return errNoOrDupGoField(ptr, f.Name)
		}
		if seen[f.Name] {
			// We don't allow duplicated field name.
			return errDupSpannerField(f.Name, ty)
		}
		// Try to decode a single field.
		if err := decodeValue(pb.Values[i], f.Type, v.FieldByIndex(sf.Index).Addr().Interface()); err != nil {
			return errDecodeStructField(ty, f.Name, err)
		}
		// Mark field f.Name as processed.
		seen[f.Name] = true
	}
	return nil
}

// isPtrStructPtrSlice returns true if ptr is a pointer to a slice of struct pointers.
func isPtrStructPtrSlice(t reflect.Type) bool {
	if t.Kind() != reflect.Ptr || t.Elem().Kind() != reflect.Slice {
		// t is not a pointer to a slice.
		return false
	}
	if t = t.Elem(); t.Elem().Kind() != reflect.Ptr || t.Elem().Elem().Kind() != reflect.Struct {
		// the slice that t points to is not a slice of struct pointers.
		return false
	}
	return true
}

// decodeStructArray decodes tspb.ListValue pb into struct slice referenced by pointer ptr, according to the
// structual information given in a tspb.StructType.
func decodeStructArray(ty *tspb.StructType, pb *tspb.ListValue, ptr interface{}) error {
	if pb == nil {
		return errNilListValue("STRUCT")
	}
	// Type of the struct pointers stored in the slice that ptr points to.
	ts := reflect.TypeOf(ptr).Elem().Elem()
	// The slice that ptr points to, might be nil at this point.
	v := reflect.ValueOf(ptr).Elem()
	// Allocate empty slice.
	v.Set(reflect.MakeSlice(v.Type(), 0, len(pb.Values)))
	// Decode every struct in pb.Values.
	for i, pv := range pb.Values {
		// Check if pv is a NULL value.
		if _, isNull := pv.Kind.(*tspb.Value_NullValue); isNull {
			// Append a nil pointer to the slice.
			v.Set(reflect.Append(v, reflect.New(ts).Elem()))
			continue
		}
		// Allocate empty struct.
		s := reflect.New(ts.Elem())
		// Get tspb.ListValue l from tspb.Value pv.
		l, err := getListValue(pv)
		if err != nil {
			return errDecodeArrayElement(i, pv, "STRUCT", err)
		}
		// Decode tspb.ListValue l into struct referenced by s.Interface().
		if err = decodeStruct(ty, l, s.Interface()); err != nil {
			return errDecodeArrayElement(i, pv, "STRUCT", err)
		}
		// Append the decoded struct back into the slice.
		v.Set(reflect.Append(v, s))
	}
	return nil
}

// errEncoderUnsupportedType returns error for not being able to encode a value of
// certain type.
func errEncoderUnsupportedType(v interface{}) error {
	return wrapError(codes.InvalidArgument, "encoder doesn't support type %T", v)
}

// 将 Go 原生类型编码成为 protobuf 的 tspb.Value，以及自定义的 type
func encodeValue(v interface{}) (*tspb.Value, *tspb.Type, error) {
	pb := &tspb.Value{
		Kind: &tspb.Value_NullValue{NullValue: tspb.NullValue_NULL_VALUE},
	}
	var pt *tspb.Type
	var err error
	switch v := v.(type) {
	case nil:
	case string:
		pb.Kind = stringKind(v)
		pt = stringType()
	case NullString:
		if v.Valid {
			return encodeValue(v.StringVal)
		}
	case []string:
		if v != nil {
			pb, err = encodeArray(len(v), func(i int) interface{} { return v[i] })
			if err != nil {
				return nil, nil, err
			}
			pt = listType(stringType())
		}
	case []NullString:
		if v != nil {
			pb, err = encodeArray(len(v), func(i int) interface{} { return v[i] })
			if err != nil {
				return nil, nil, err
			}
			pt = listType(stringType())
		}
	case []byte:
		if v != nil {
			// pb.Kind = stringKind(base64.StdEncoding.EncodeToString(v))
			pb.Kind = bytesKind(v)
			pt = bytesType()
		}
	case [][]byte:
		if v != nil {
			pb, err = encodeArray(len(v), func(i int) interface{} { return v[i] })
			if err != nil {
				return nil, nil, err
			}
			pt = listType(bytesType())
		}
	case int:
		// pb.Kind = stringKind(strconv.FormatInt(int64(v), 10))
		pb.Kind = &tspb.Value_IntegerValue{IntegerValue: int64(v)}
		pt = intType()
	case []int:
		if v != nil {
			pb, err = encodeArray(len(v), func(i int) interface{} { return v[i] })
			if err != nil {
				return nil, nil, err
			}
			pt = listType(intType())
		}
	case int64:
		// pb.Kind = stringKind(strconv.FormatInt(v, 10))
		pb.Kind = &tspb.Value_IntegerValue{IntegerValue: int64(v)}
		pt = intType()
	case []int64:
		if v != nil {
			pb, err = encodeArray(len(v), func(i int) interface{} { return v[i] })
			if err != nil {
				return nil, nil, err
			}
			pt = listType(intType())
		}
	case NullInt64:
		if v.Valid {
			return encodeValue(v.Int64)
		}
	case []NullInt64:
		if v != nil {
			pb, err = encodeArray(len(v), func(i int) interface{} { return v[i] })
			if err != nil {
				return nil, nil, err
			}
			pt = listType(intType())
		}
	case bool:
		pb.Kind = &tspb.Value_BoolValue{BoolValue: v}
		pt = boolType()
	case []bool:
		if v != nil {
			pb, err = encodeArray(len(v), func(i int) interface{} { return v[i] })
			if err != nil {
				return nil, nil, err
			}
			pt = listType(boolType())
		}
	case NullBool:
		if v.Valid {
			return encodeValue(v.Bool)
		}
	case []NullBool:
		if v != nil {
			pb, err = encodeArray(len(v), func(i int) interface{} { return v[i] })
			if err != nil {
				return nil, nil, err
			}
			pt = listType(boolType())
		}
	case float64:
		pb.Kind = &tspb.Value_NumberValue{NumberValue: v}
		pt = floatType()
	case []float64:
		if v != nil {
			pb, err = encodeArray(len(v), func(i int) interface{} { return v[i] })
			if err != nil {
				return nil, nil, err
			}
			pt = listType(floatType())
		}
	case NullFloat64:
		if v.Valid {
			return encodeValue(v.Float64)
		}
	case []NullFloat64:
		if v != nil {
			pb, err = encodeArray(len(v), func(i int) interface{} { return v[i] })
			if err != nil {
				return nil, nil, err
			}
			pt = listType(floatType())
		}
	case time.Time:
		// pb.Kind = stringKind(v.UTC().Format(time.RFC3339Nano))
		pb.Kind = timeKind(v)
		pt = timeType()
	case []time.Time:
		if v != nil {
			pb, err = encodeArray(len(v), func(i int) interface{} { return v[i] })
			if err != nil {
				return nil, nil, err
			}
			pt = listType(timeType())
		}
	case NullTime:
		if v.Valid {
			return encodeValue(v.Time)
		}
	case []NullTime:
		if v != nil {
			pb, err = encodeArray(len(v), func(i int) interface{} { return v[i] })
			if err != nil {
				return nil, nil, err
			}
			pt = listType(timeType())
		}
	case civil.Date:
		// pb.Kind = stringKind(v.String())
		pb.Kind = DateKind(v)
		pt = dateType()
	case []civil.Date:
		if v != nil {
			pb, err = encodeArray(len(v), func(i int) interface{} { return v[i] })
			if err != nil {
				return nil, nil, err
			}
			pt = listType(dateType())
		}
	case NullDate:
		if v.Valid {
			return encodeValue(v.Date)
		}
	case []NullDate:
		if v != nil {
			pb, err = encodeArray(len(v), func(i int) interface{} { return v[i] })
			if err != nil {
				return nil, nil, err
			}
			pt = listType(dateType())
		}
	case GenericColumnValue:
		// Deep clone to ensure subsequent changes to v before
		// transmission don't affect our encoded value.
		pb = proto.Clone(v.Value).(*tspb.Value)
		pt = proto.Clone(v.Type).(*tspb.Type)
	default:
		return nil, nil, errEncoderUnsupportedType(v)
	}
	return pb, pt, nil
}

// 将原生数组 encode 为 list
// encodeValueArray encodes a Value array into a tspb.ListValue.
func encodeValueArray(vs []interface{}) (*tspb.ListValue, error) {
	lv := &tspb.ListValue{}
	lv.Values = make([]*tspb.Value, 0, len(vs))
	for _, v := range vs {
		pb, _, err := encodeValue(v)
		if err != nil {
			return nil, err
		}
		lv.Values = append(lv.Values, pb)
	}
	return lv, nil
}

// 前提是数组各元素都能 encode
// encodeArray assumes that all values of the array element type encode without error.
func encodeArray(len int, at func(int) interface{}) (*tspb.Value, error) {
	vs := make([]*tspb.Value, len)
	var err error
	for i := 0; i < len; i++ {
		vs[i], _, err = encodeValue(at(i))
		if err != nil {
			return nil, err
		}
	}
	return listProto(vs...), nil
}

func spannerTagParser(t reflect.StructTag) (name string, keep bool, other interface{}, err error) {
	if s := t.Get("spanner"); s != "" {
		if s == "-" {
			return "", false, nil, nil
		}
		return s, true, nil, nil
	}
	return "", true, nil, nil
}

func zettaTagParser(t reflect.StructTag) (name string, keep bool, other interface{}, err error) {
	family := t.Get("family")
	column := t.Get("column")
	if column != "" {
		if column == "-" {
			return "", false, nil, nil
		}
		if family != "" {
			return family + ":" + column, true, nil, nil
		}
		return column, true, nil, nil
	}
	return "", true, nil, nil
}

var fieldCache = fields.NewCache(zettaTagParser, nil, nil)
