package go_ora

import (
	"bytes"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sijms/go-ora/v2/converters"
	"github.com/sijms/go-ora/v2/network"
)

var (
	tyBool            = reflect.TypeOf((*bool)(nil)).Elem()
	tyBytes           = reflect.TypeOf((*[]byte)(nil)).Elem()
	tyString          = reflect.TypeOf((*string)(nil)).Elem()
	tyNVarChar        = reflect.TypeOf((*NVarChar)(nil)).Elem()
	tyTime            = reflect.TypeOf((*time.Time)(nil)).Elem()
	tyTimeStamp       = reflect.TypeOf((*TimeStamp)(nil)).Elem()
	tyTimeStampTZ     = reflect.TypeOf((*TimeStampTZ)(nil)).Elem()
	tyClob            = reflect.TypeOf((*Clob)(nil)).Elem()
	tyNClob           = reflect.TypeOf((*NClob)(nil)).Elem()
	tyBlob            = reflect.TypeOf((*Blob)(nil)).Elem()
	tyBFile           = reflect.TypeOf((*BFile)(nil)).Elem()
	tyNullByte        = reflect.TypeOf((*sql.NullByte)(nil)).Elem()
	tyNullInt16       = reflect.TypeOf((*sql.NullInt16)(nil)).Elem()
	tyNullInt32       = reflect.TypeOf((*sql.NullInt32)(nil)).Elem()
	tyNullInt64       = reflect.TypeOf((*sql.NullInt64)(nil)).Elem()
	tyNullFloat64     = reflect.TypeOf((*sql.NullFloat64)(nil)).Elem()
	tyNullBool        = reflect.TypeOf((*sql.NullBool)(nil)).Elem()
	tyNullString      = reflect.TypeOf((*sql.NullString)(nil)).Elem()
	tyNullNVarChar    = reflect.TypeOf((*NullNVarChar)(nil)).Elem()
	tyNullTime        = reflect.TypeOf((*sql.NullTime)(nil)).Elem()
	tyNullTimeStamp   = reflect.TypeOf((*NullTimeStamp)(nil)).Elem()
	tyNullTimeStampTZ = reflect.TypeOf((*NullTimeStampTZ)(nil)).Elem()
	tyRefCursor       = reflect.TypeOf((*RefCursor)(nil)).Elem()
	tyPLBool          = reflect.TypeOf((*PLBool)(nil)).Elem()
	tyObject          = reflect.TypeOf((*Object)(nil)).Elem()
	tyNumber          = reflect.TypeOf((*Number)(nil)).Elem()
)

func refineSqlText(text string) string {
	index := 0
	length := len(text)
	skip := false
	lineComment := false
	textBuffer := make([]byte, 0, len(text))
	for ; index < length; index++ {
		ch := text[index]
		switch ch {
		case '\\':
			// bypass next character
			continue
		case '/':
			if index+1 < length && text[index+1] == '*' {
				index += 1
				skip = true
			}
		case '*':
			if index+1 < length && text[index+1] == '/' {
				index += 1
				skip = false
			}
		case '\'':
			skip = !skip
		case '"':
			skip = !skip
		case '-':
			if !skip {
				if index+1 < length && text[index+1] == '-' {
					index += 1
					lineComment = true
				}
			}
		case '\n':
			//if lineComment {
			//	lineComment = false
			//}
			if lineComment {
				lineComment = false
			} else {
				textBuffer = append(textBuffer, ch) // oheurtel : keep the line feed character
			}
		default:
			if skip || lineComment {
				continue
			}
			textBuffer = append(textBuffer, text[index])
		}
	}
	return strings.TrimSpace(string(textBuffer))
}
func parseSqlText(text string) ([]string, error) {
	refinedSql := refineSqlText(text)
	reg, err := regexp.Compile(`:(\w+)`)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, 10)
	matches := reg.FindAllStringSubmatch(refinedSql, -1)
	for _, match := range matches {
		if len(match) > 1 {
			names = append(names, match[1])
		}
	}
	return names, nil
}

func extractTag(tag string) (name, _type string, size int, direction ParameterDirection) {
	extractNameValue := func(input string, pos int) {
		parts := strings.Split(input, "=")
		var id, value string
		if len(parts) < 2 {
			switch pos {
			case 0:
				id = "name"
			case 1:
				id = "type"
			case 2:
				id = "size"
			case 3:
				id = "direction"
			}
			value = input
		} else {
			id = strings.TrimSpace(strings.ToLower(parts[0]))
			value = strings.TrimSpace(parts[1])
		}
		switch id {
		case "name":
			name = value
		case "type":
			_type = value
		case "size":
			tempSize, _ := strconv.ParseInt(value, 10, 32)
			size = int(tempSize)
		case "dir":
			fallthrough
		case "direction":
			switch value {
			case "in", "input":
				direction = Input
			case "out", "output":
				direction = Output
			case "inout":
				direction = InOut
			}
		}
	}
	tag = strings.TrimSpace(tag)
	if len(tag) == 0 {
		return
	}
	tagFields := strings.Split(tag, ",")
	if len(tagFields) > 0 {
		extractNameValue(tagFields[0], 0)
	}
	if len(tagFields) > 1 {
		extractNameValue(tagFields[1], 1)
	}
	if len(tagFields) > 2 {
		extractNameValue(tagFields[2], 2)
	}
	if len(tagFields) > 3 {
		extractNameValue(tagFields[3], 3)
	}
	return
}

func tSigned(input reflect.Type) bool {
	switch input.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	default:
		return false
	}
}
func tUnsigned(input reflect.Type) bool {
	switch input.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	default:
		return false
	}
}
func tInteger(input reflect.Type) bool {
	return tSigned(input) || tUnsigned(input)
}
func tFloat(input reflect.Type) bool {
	return input.Kind() == reflect.Float32 || input.Kind() == reflect.Float64
}
func tNumber(input reflect.Type) bool {
	return tInteger(input) || tFloat(input) || input == tyBool
}
func tNullNumber(input reflect.Type) bool {
	switch input {
	case tyNullBool, tyNullByte, tyNullInt16, tyNullInt32, tyNullInt64:
		fallthrough
	case tyNullFloat64:
		return true
	}
	return false
}

//======== get primitive data from original data types ========//

// get value to bypass pointer and sql.Null* values
func getValue(origVal driver.Value) (driver.Value, error) {
	if origVal == nil {
		return nil, nil
	}
	rOriginal := reflect.ValueOf(origVal)
	if rOriginal.Kind() == reflect.Ptr && rOriginal.IsNil() {
		return nil, nil
	}
	proVal := reflect.Indirect(rOriginal)
	if valuer, ok := proVal.Interface().(driver.Valuer); ok {
		return valuer.Value()
	}
	return proVal.Interface(), nil
}

// get prim string value from supported types
func getString(col interface{}) string {
	col, _ = getValue(col)
	if col == nil {
		return ""
	}
	switch val := col.(type) {
	case Clob:
		return val.String
	case NClob:
		return val.String
	}
	if temp, ok := col.(string); ok {
		return temp
	} else {
		return fmt.Sprintf("%v", col)
	}
}

func getBool(col interface{}) (bool, error) {
	col, err := getValue(col)
	if err != nil {
		return false, err
	}
	if col == nil {
		return false, nil
	}
	rValue := reflect.ValueOf(col)
	return rValue.Bool(), nil
}

//func getNumber(col interface{}) (*Number, error) {
//
//}

// get prim float64 from supported types
//func getFloat(col interface{}) (float64, error) {
//	var err error
//	col, err = getValue(col)
//	if err != nil {
//		return 0, err
//	}
//	if col == nil {
//		return 0, nil
//	}
//	rType := reflect.TypeOf(col)
//	rValue := reflect.ValueOf(col)
//	if tInteger(rType) {
//		return float64(rValue.Int()), nil
//	}
//	if f32, ok := col.(float32); ok {
//		return strconv.ParseFloat(fmt.Sprint(f32), 64)
//	}
//	if tFloat(rType) {
//		return rValue.Float(), nil
//	}
//	switch rType.Kind() {
//	case reflect.Bool:
//		if rValue.Bool() {
//			return 1, nil
//		} else {
//			return 0, nil
//		}
//	case reflect.String:
//		tempFloat, err := strconv.ParseFloat(rValue.String(), 64)
//		if err != nil {
//			return 0, err
//		}
//		return tempFloat, nil
//	default:
//		return 0, errors.New("conversion of unsupported type to float")
//	}
//}

// get prim int64 value from supported types
func getInt(col interface{}) (int64, error) {
	var err error
	col, err = getValue(col)
	if err != nil {
		return 0, err
	}
	if col == nil {
		return 0, nil
	}
	rType := reflect.TypeOf(col)
	rValue := reflect.ValueOf(col)
	if tInteger(rType) {
		return rValue.Int(), nil
	}
	if tFloat(rType) {
		return int64(rValue.Float()), nil
	}
	switch rType.Kind() {
	case reflect.String:
		tempInt, err := strconv.ParseInt(rValue.String(), 10, 64)
		if err != nil {
			return 0, err
		}
		return tempInt, nil
	case reflect.Bool:
		if rValue.Bool() {
			return 1, nil
		} else {
			return 0, nil
		}
	default:
		return 0, errors.New("conversion of unsupported type to int")
	}
}

// get prim time.Time from supported types
func getDate(col interface{}) (time.Time, error) {
	var err error
	col, err = getValue(col)
	if err != nil {
		return time.Time{}, err
	}
	if col == nil {
		return time.Time{}, nil
	}
	switch val := col.(type) {
	case time.Time:
		return val, nil
	case TimeStamp:
		return time.Time(val), nil
	case TimeStampTZ:
		return time.Time(val), nil
	case string:
		return time.Parse(time.RFC3339, val)
	default:
		return time.Time{}, errors.New("conversion of unsupported type to time.Time")
	}
}

// get prim []byte from supported types
func getBytes(col interface{}) ([]byte, error) {
	var err error
	col, err = getValue(col)
	if err != nil {
		return nil, err
	}
	if col == nil {
		return nil, nil
	}
	switch val := col.(type) {
	case []byte:
		return val, nil
	case string:
		return []byte(val), nil
	case Blob:
		return val.Data, nil
	default:
		return nil, errors.New("conversion of unsupported type to []byte")
	}
}

// get prim lob from supported types
func getLob(col interface{}, conn *Connection) (*Lob, error) {
	var err error
	col, err = getValue(col)
	if err != nil {
		return nil, err
	}
	if col == nil {
		return nil, nil
	}
	charsetID := conn.tcpNego.ServerCharset
	charsetForm := 1
	stringVar := ""
	var byteVar []byte
	switch val := col.(type) {
	case string:
		stringVar = val
	case Clob:
		if !val.Valid {
			return nil, nil
		}
		stringVar = val.String
	case NVarChar:
		stringVar = string(val)
		charsetForm = 2
		charsetID = conn.tcpNego.ServernCharset
	case NClob:
		charsetForm = 2
		charsetID = conn.tcpNego.ServernCharset
		if !val.Valid {
			return nil, nil
		}
		stringVar = val.String
	case []byte:
		byteVar = val
	case Blob:
		byteVar = val.Data
	}
	if len(stringVar) > 0 {
		lob := newLob(conn)
		err = lob.createTemporaryClob(charsetID, charsetForm)
		if err != nil {
			return nil, err
		}
		err = lob.putString(stringVar)
		return lob, err
	}
	if len(byteVar) > 0 {
		lob := newLob(conn)
		err = lob.createTemporaryBLOB()
		if err != nil {
			return nil, err
		}
		err = lob.putData(byteVar)
		return lob, err
	}
	return nil, nil
}

//=============================================================//

func setBytes(value reflect.Value, input []byte) error {
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		return setBytes(value.Elem(), input)
	}
	switch value.Type() {
	case tyString:
		value.SetString(string(input))
	case tyBytes:
		value.SetBytes(input)
	case tyNVarChar:
		value.Set(reflect.ValueOf(NVarChar(input)))
	case tyBlob:
		value.Set(reflect.ValueOf(Blob{Data: input}))
	case tyClob:
		value.Set(reflect.ValueOf(Clob{String: string(input), Valid: true}))
	case tyNClob:
		value.Set(reflect.ValueOf(NClob{String: string(input), Valid: true}))
	case tyNullString:
		value.Set(reflect.ValueOf(sql.NullString{string(input), true}))
	case tyNullNVarChar:
		value.Set(reflect.ValueOf(NullNVarChar{NVarChar(input), true}))
	default:
		if temp, ok := value.Interface().(sql.Scanner); ok {
			if temp != nil && !reflect.ValueOf(temp).IsNil() {
				return temp.Scan(input)
			}
		}
		if value.CanAddr() {
			if temp, ok := value.Addr().Interface().(sql.Scanner); ok {
				err := temp.Scan(input)
				return err
			}
		}
		return fmt.Errorf("can not assign []byte to type: %v", value.Type().Name())
	}
	return nil
}
func setTime(value reflect.Value, input time.Time) error {
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		return setTime(value.Elem(), input)
	}
	switch value.Type() {
	case tyString:
		value.SetString(input.Format(time.RFC3339))
	case tyTime:
		value.Set(reflect.ValueOf(input))
	case tyTimeStamp:
		value.Set(reflect.ValueOf(TimeStamp(input)))
	case tyTimeStampTZ:
		value.Set(reflect.ValueOf(TimeStampTZ(input)))
	case tyNullString:
		value.Set(reflect.ValueOf(sql.NullString{input.Format(time.RFC3339), true}))
	case tyNullTime:
		value.Set(reflect.ValueOf(sql.NullTime{input, true}))
	case tyNullTimeStamp:
		value.Set(reflect.ValueOf(NullTimeStamp{TimeStamp(input), true}))
	case tyNullTimeStampTZ:
		value.Set(reflect.ValueOf(NullTimeStampTZ{TimeStampTZ(input), true}))
	default:
		if temp, ok := value.Interface().(sql.Scanner); ok {
			if temp != nil && !reflect.ValueOf(temp).IsNil() {
				return temp.Scan(input)
			}
		}
		if value.CanAddr() {
			if temp, ok := value.Addr().Interface().(sql.Scanner); ok {
				err := temp.Scan(input)
				return err
			}
		}
		return fmt.Errorf("can not assign time to type: %v", value.Type().Name())
	}
	return nil
}
func setFieldValue(fieldValue reflect.Value, cust *customType, input interface{}) error {

	//input should be one of primitive values
	if input == nil {
		return setNull(fieldValue)
	}
	if fieldValue.Kind() == reflect.Ptr && fieldValue.Elem().Kind() == reflect.Interface {
		fieldValue.Elem().Set(reflect.ValueOf(input))
		return nil
	}
	if fieldValue.Kind() == reflect.Interface {
		fieldValue.Set(reflect.ValueOf(input))
		return nil
	}
	//if fieldValue.CanAddr() {
	//	if scan, ok := fieldValue.Addr().Interface().(sql.Scanner); ok {
	//		return scan.Scan(input)
	//	}
	//} else {
	//	if scan, ok := fieldValue.Interface().(sql.Scanner); ok {
	//		return scan.Scan(input)
	//	}
	//}

	switch val := input.(type) {
	case int64, float64:
		num, err := NewNumber(val)
		if err != nil {
			return err
		}
		return setNumber(fieldValue, num)
	//case float64:
	//	return setNumber(fieldValue, val)
	case string:
		return setString(fieldValue, val)
	case time.Time:
		return setTime(fieldValue, val)
	case []byte:
		return setBytes(fieldValue, val)
	case Lob:
		return setLob(fieldValue, val)
	case BFile:
		return setBFile(fieldValue, val)
	case []ParameterInfo:
		return setUDTObject(fieldValue, cust, val)
	default:
		if temp, ok := fieldValue.Interface().(sql.Scanner); ok {
			if temp != nil && !reflect.ValueOf(temp).IsNil() {
				return temp.Scan(input)
			}
		}
		if fieldValue.CanAddr() {
			if temp, ok := fieldValue.Addr().Interface().(sql.Scanner); ok {
				err := temp.Scan(input)
				return err
			}
		}
		return fmt.Errorf("unsupported primitive type: %s", fieldValue.Type().Name())
	}
}
func setNull(value reflect.Value) error {
	if value.Kind() == reflect.Ptr && value.IsNil() {
		return nil
	}
	value.Set(reflect.Zero(value.Type()))
	//value.SetZero()
	return nil
}
func setBFile(value reflect.Value, input BFile) error {
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		return setBFile(value.Elem(), input)
	}
	switch value.Type() {
	case tyBFile:
		value.Set(reflect.ValueOf(input))
	default:
		if temp, ok := value.Interface().(sql.Scanner); ok {
			if temp != nil && !reflect.ValueOf(temp).IsNil() {
				return temp.Scan(input)
			}
		}
		if value.CanAddr() {
			if temp, ok := value.Addr().Interface().(sql.Scanner); ok {
				err := temp.Scan(input)
				return err
			}
		}
		return fmt.Errorf("can't assign BFILE to type: %v", value.Type().Name())
	}
	return nil
}
func setArray(value reflect.Value, input []ParameterInfo) error {
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		return setArray(value.Elem(), input)
	}
	tValue := value.Type()
	tempSlice := reflect.MakeSlice(tValue, 0, len(input))
	for _, par := range input {
		tempObj := reflect.New(tValue.Elem())
		err := setFieldValue(tempObj.Elem(), par.cusType, par.oPrimValue)
		if err != nil {
			return err
		}
		tempSlice = reflect.Append(tempSlice, tempObj.Elem())
	}
	value.Set(tempSlice)
	return nil
}

func setUDTObject(value reflect.Value, cust *customType, input []ParameterInfo) error {
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		return setUDTObject(value.Elem(), cust, input)
	}
	if value.Kind() == reflect.Slice || value.Kind() == reflect.Array {
		return setArray(value, input)
		//if cust.isRegularArray() {
		//
		//} else {
		//	arrayObj := reflect.MakeSlice(reflect.SliceOf(cust.typ), 0, len(input))
		//	for _, par := range input {
		//		if temp, ok := par.oPrimValue.([]ParameterInfo); ok {
		//			tempObj2 := reflect.New(cust.typ)
		//			err := setFieldValue(tempObj2.Elem(), par.cusType, temp)
		//			if err != nil {
		//				return err
		//			}
		//			arrayObj = reflect.Append(arrayObj, tempObj2.Elem())
		//		}
		//	}
		//	value.Set(arrayObj)
		//}
	} else {
		tempObj := reflect.New(cust.typ)
		for _, par := range input {

			if fieldIndex, ok := cust.fieldMap[par.Name]; ok {
				err := setFieldValue(tempObj.Elem().Field(fieldIndex), par.cusType, par.oPrimValue)
				if err != nil {
					return err
				}
			}
		}
		value.Set(tempObj.Elem())
	}
	return nil
}

func setLob(value reflect.Value, input Lob) error {
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		return setLob(value.Elem(), input)
	}
	//dataSize, err := input.getSize()
	//if err != nil {
	//	return err
	//}
	if input.connection == nil || len(input.sourceLocator) == 0 {
		return setNull(value)
	}
	lobData, err := input.getData()
	if err != nil {
		return err
	}
	conn := input.connection
	if len(lobData) == 0 {
		return setNull(value)
	}
	getStrConv := func() (converters.IStringConverter, error) {
		var ret converters.IStringConverter
		if input.variableWidthChar() {
			if conn.dBVersion.Number < 10200 && input.littleEndianClob() {
				ret, _ = conn.getStrConv(2002)
			} else {
				ret, _ = conn.getStrConv(2000)
			}
		} else {
			ret, err = conn.getStrConv(input.charsetID)
			if err != nil {
				return nil, err
			}
		}
		return ret, nil
	}
	var strConv converters.IStringConverter
	switch value.Type() {
	case tyString:
		strConv, err = getStrConv()
		if err != nil {
			return err
		}
		value.SetString(strConv.Decode(lobData))
	case tyNullString:
		strConv, err = getStrConv()
		if err != nil {
			return err
		}
		value.Set(reflect.ValueOf(sql.NullString{strConv.Decode(lobData), true}))
	case tyNVarChar:
		strConv, err = getStrConv()
		if err != nil {
			return err
		}
		value.Set(reflect.ValueOf(NVarChar(strConv.Decode(lobData))))
	case tyNullNVarChar:
		strConv, err = getStrConv()
		if err != nil {
			return err
		}
		value.Set(reflect.ValueOf(NullNVarChar{NVarChar(strConv.Decode(lobData)), true}))
	case tyClob:
		strConv, err = getStrConv()
		if err != nil {
			return err
		}
		value.Set(reflect.ValueOf(Clob{
			String:  strConv.Decode(lobData),
			Valid:   true,
			locator: input.sourceLocator}))
	case tyNClob:
		strConv, err = getStrConv()
		if err != nil {
			return err
		}
		value.Set(reflect.ValueOf(NClob{
			String:  strConv.Decode(lobData),
			Valid:   true,
			locator: input.sourceLocator}))
	case tyBlob:
		value.Set(reflect.ValueOf(Blob{
			Data:    lobData,
			locator: input.sourceLocator}))
	case tyBytes:
		value.Set(reflect.ValueOf(lobData))
	default:
		if temp, ok := value.Interface().(sql.Scanner); ok {
			if temp != nil && !reflect.ValueOf(temp).IsNil() {
				return temp.Scan(lobData)
			}
		}
		if value.CanAddr() {
			if temp, ok := value.Addr().Interface().(sql.Scanner); ok {
				err := temp.Scan(lobData)
				return err
			}
		}
		return fmt.Errorf("can't assign LOB to type: %v", value.Type().Name())
	}
	return nil
}

func setString(value reflect.Value, input string) error {
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		return setString(value.Elem(), input)
	}
	var intErr, floatErr, timeErr error
	tempInt, err := strconv.ParseInt(input, 10, 64)
	if err != nil {
		intErr = fmt.Errorf(`can't assign string "%v" to int variable`, input)
	}
	tempFloat, err := strconv.ParseFloat(input, 64)
	if err != nil {
		floatErr = fmt.Errorf(`can't assign string "%v" to float variablle`, input)
	}
	tempTime, err := time.Parse(time.RFC3339, input)
	if err != nil {
		timeErr = fmt.Errorf(`can't assign string "%v" to time.Time variable`, input)
	}
	if tSigned(value.Type()) {
		if intErr == nil {
			value.SetInt(tempInt)
		}
		return intErr
	}
	if tUnsigned(value.Type()) {
		if intErr == nil {
			value.SetUint(uint64(tempInt))
		}
		return intErr
	}
	if tFloat(value.Type()) {
		if floatErr == nil {
			value.SetFloat(tempFloat)
		}
		return floatErr
	}
	switch value.Type() {
	case tyNumber:
		tempNum, err := NewNumberFromString(input)
		if err != nil {
			return err
		}
		value.Set(reflect.ValueOf(*tempNum))
	case tyBool:
		if strings.ToLower(input) == "true" {
			value.SetBool(true)
		} else {
			value.SetBool(false)
		}
	case tyString:
		value.SetString(input)
	case tyNullString:
		value.Set(reflect.ValueOf(sql.NullString{input, true}))
	case tyNullByte:
		if intErr == nil {
			value.Set(reflect.ValueOf(sql.NullByte{uint8(tempInt), true}))
		}
		return intErr
	case tyNullInt16:
		if intErr == nil {
			value.Set(reflect.ValueOf(sql.NullInt16{int16(tempInt), true}))
		}
		return intErr
	case tyNullInt32:
		if intErr == nil {
			value.Set(reflect.ValueOf(sql.NullInt32{int32(tempInt), true}))
		}
		return intErr
	case tyNullInt64:
		if intErr == nil {
			value.Set(reflect.ValueOf(sql.NullInt64{tempInt, true}))
		}
		return intErr
	case tyNullFloat64:
		if floatErr == nil {
			value.Set(reflect.ValueOf(sql.NullFloat64{tempFloat, true}))
		}
		return floatErr
	case tyNullBool:
		temp := strings.ToLower(input) == "true"
		value.Set(reflect.ValueOf(sql.NullBool{temp, true}))
	case tyNVarChar:
		value.Set(reflect.ValueOf(NVarChar(input)))
	case tyNullNVarChar:
		value.Set(reflect.ValueOf(NullNVarChar{NVarChar(input), true}))
	case tyTime:
		if timeErr == nil {
			value.Set(reflect.ValueOf(tempTime))
		}
		return timeErr
	case tyNullTime:
		if timeErr == nil {
			value.Set(reflect.ValueOf(sql.NullTime{tempTime, true}))
		}
		return timeErr
	case tyTimeStamp:
		if timeErr == nil {
			value.Set(reflect.ValueOf(TimeStamp(tempTime)))
		}
		return timeErr
	case tyNullTimeStamp:
		if timeErr == nil {
			value.Set(reflect.ValueOf(NullTimeStamp{TimeStamp(tempTime), true}))
		}
		return timeErr
	case tyTimeStampTZ:
		if timeErr == nil {
			value.Set(reflect.ValueOf(TimeStampTZ(tempTime)))
		}
		return timeErr
	case tyNullTimeStampTZ:
		if timeErr == nil {
			value.Set(reflect.ValueOf(NullTimeStampTZ{TimeStampTZ(tempTime), true}))
		}
		return timeErr
	case tyClob:
		value.Set(reflect.ValueOf(Clob{String: input, Valid: true}))
	case tyNClob:
		value.Set(reflect.ValueOf(NClob{String: input, Valid: true}))
	default:
		if temp, ok := value.Interface().(sql.Scanner); ok {
			if temp != nil && !reflect.ValueOf(temp).IsNil() {
				return temp.Scan(input)
			}
		}
		if value.CanAddr() {
			if temp, ok := value.Addr().Interface().(sql.Scanner); ok {
				err := temp.Scan(input)
				return err
			}
		}
		return fmt.Errorf("can not assign string to type: %v", value.Type().Name())
	}
	return nil
}

//	func setInt(value reflect.Value, input int64) error {
//		if value.Kind() == reflect.Ptr && value.IsNil() {
//			value.Set(reflect.New(value.Type().Elem()))
//			return setInt(value.Elem(), input)
//		}
//		if tSigned(value.Type()) {
//			value.SetInt(input)
//			return nil
//		}
//		if tUnsigned(value.Type()) {
//			value.SetUint(uint64(input))
//			return nil
//		}
//		if tFloat(value.Type()) {
//			value.SetFloat(float64(input))
//			return nil
//		}
//	}

func setNumber(value reflect.Value, input *Number) error {
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		return setNumber(value.Elem(), input)
	}
	if tSigned(value.Type()) {
		temp, err := input.Int64()
		if err != nil {
			return err
		}
		value.SetInt(temp)
		return nil
	}
	if tUnsigned(value.Type()) {
		temp, err := input.Uint64()
		if err != nil {
			return err
		}
		value.SetUint(temp)
		return nil
	}
	if tFloat(value.Type()) {
		temp, err := input.Float64()
		if err != nil {
			return err
		}
		value.SetFloat(temp)
		return nil
	}
	switch value.Type() {
	case tyBool:
		value.SetBool(!input.isZero())
	case tyString:
		temp, err := input.String()
		if err != nil {
			return err
		}
		value.SetString(temp)
	case tyNullString:
		temp, err := input.String()
		if err != nil {
			return err
		}
		value.Set(reflect.ValueOf(sql.NullString{temp, true}))
	case tyNullByte:
		temp, err := input.Int64()
		if err != nil {
			return err
		}
		value.Set(reflect.ValueOf(sql.NullByte{uint8(temp), true}))
	case tyNullInt16:
		temp, err := input.Int64()
		if err != nil {
			return err
		}
		value.Set(reflect.ValueOf(sql.NullInt16{int16(temp), true}))
	case tyNullInt32:
		temp, err := input.Int64()
		if err != nil {
			return err
		}
		value.Set(reflect.ValueOf(sql.NullInt32{int32(temp), true}))
	case tyNullInt64:
		temp, err := input.Int64()
		if err != nil {
			return err
		}
		value.Set(reflect.ValueOf(sql.NullInt64{temp, true}))
	case tyNullFloat64:
		temp, err := input.Float64()
		if err != nil {
			return err
		}
		value.Set(reflect.ValueOf(sql.NullFloat64{temp, true}))
	case tyNullBool:
		value.Set(reflect.ValueOf(sql.NullBool{!input.isZero(), true}))
	case tyNullNVarChar:
		temp, err := input.String()
		if err != nil {
			return err
		}
		value.Set(reflect.ValueOf(NullNVarChar{NVarChar(temp), true}))
	default:
		if temp, ok := value.Interface().(sql.Scanner); ok {
			if temp != nil && !reflect.ValueOf(temp).IsNil() {
				return temp.Scan(input)
			}
		}
		if value.CanAddr() {
			if temp, ok := value.Addr().Interface().(sql.Scanner); ok {
				err := temp.Scan(input)
				return err
			}
		}
		return fmt.Errorf("can not assign number to type: %v", value.Type().Name())
	}
	return nil
}

func isBadConn(err error) bool {
	var opError *net.OpError
	var oraError *network.OracleError
	switch {
	case errors.Is(err, io.EOF):
		return true
	case errors.Is(err, syscall.EPIPE):
		return true
	case errors.As(err, &opError):
		if opError.Net == "tcp" && opError.Op == "write" {
			return true
		}
	case errors.As(err, &oraError):
		return oraError.Bad()
	}
	return false
}

//func collectLocators(pars []ParameterInfo) [][]byte {
//	output := make([][]byte, 0, 10)
//	for _, par := range pars {
//		output = append(output, par.collectLocator()...)
//switch value := par.iPrimValue.(type) {
//case *Lob:
//	if value != nil && value.sourceLocator != nil {
//		output = append(output, value.sourceLocator)
//	}
//case *BFile:
//	if value != nil && value.lob.sourceLocator != nil {
//		output = append(output, value.lob.sourceLocator)
//	}
//case []ParameterInfo:
//	temp := collectLocators(value)
//	output = append(output, temp...)
//}
//	}
//	return output
//}

//	func initializePtr(v interface{}) {
//		rv := reflect.ValueOf(v).Elem()
//		rv.Set(reflect.New(rv.Type().Elem()))
//	}
func getTOID2(conn *sql.DB, owner, typeName string) ([]byte, error) {
	var toid []byte
	err := conn.QueryRow(`SELECT type_oid FROM ALL_TYPES WHERE UPPER(OWNER)=:1 AND UPPER(TYPE_NAME)=:2`,
		strings.ToUpper(owner), strings.ToUpper(typeName)).Scan(&toid)
	if errors.Is(err, sql.ErrNoRows) {
		err = fmt.Errorf("type: %s is not present or wrong type name", typeName)
	}
	return toid, err
}
func getTOID(conn *Connection, owner, typeName string) ([]byte, error) {
	sqlText := `SELECT type_oid FROM ALL_TYPES WHERE UPPER(OWNER)=:1 AND UPPER(TYPE_NAME)=:2`
	stmt := NewStmt(sqlText, conn)
	defer func(stmt *Stmt) {
		_ = stmt.Close()
	}(stmt)
	var ret []byte
	rows, err := stmt.Query_([]driver.NamedValue{driver.NamedValue{Value: strings.ToUpper(owner)},
		driver.NamedValue{Value: strings.ToUpper(typeName)}})
	if err != nil {
		return nil, err
	}
	if rows.Next_() {
		err = rows.Scan(&ret)
		if err != nil {
			return nil, err
		}
	}
	if len(ret) == 0 {
		return nil, fmt.Errorf("unknown type: %s", typeName)
	}
	return ret, rows.Err()
}

func encodeObject(session *network.Session, objectData []byte, isArray bool) []byte {
	size := len(objectData)
	fieldsData := bytes.Buffer{}
	if isArray {
		fieldsData.Write([]byte{0x88, 0x1})
	} else {
		fieldsData.Write([]byte{0x84, 0x1})
	}
	if (size + 7) < 0xfe {
		size += 3
		fieldsData.Write([]byte{uint8(size)})
	} else {
		size += 7
		fieldsData.Write([]byte{0xfe})
		session.WriteInt(&fieldsData, size, 4, true, false)
	}
	fieldsData.Write(objectData)
	return fieldsData.Bytes()
}
func putUDTAttributes(input *customType, pars []ParameterInfo, index int) ([]ParameterInfo, int) {
	oPrimValue := make([]ParameterInfo, 0, len(input.attribs))
	for _, attrib := range input.attribs {
		if attrib.cusType != nil && !attrib.cusType.isArray {
			var tempValue []ParameterInfo
			tempValue, index = putUDTAttributes(attrib.cusType, pars, index)
			attrib.oPrimValue = tempValue
			oPrimValue = append(oPrimValue, attrib)
		} else {
			oPrimValue = append(oPrimValue, pars[index])
			index++
		}
	}
	return oPrimValue, index
}
func getUDTAttributes(input *customType, value reflect.Value) []ParameterInfo {
	output := make([]ParameterInfo, 0, 10)
	for _, attrib := range input.attribs {
		fieldValue := reflect.Value{}
		if value.IsValid() && value.Kind() == reflect.Struct {
			if fieldIndex, ok := input.fieldMap[attrib.Name]; ok {
				fieldValue = value.Field(fieldIndex)
			}
		}
		// if attribute is a nested type and not array
		if attrib.cusType != nil && !attrib.cusType.isArray {
			output = append(output, getUDTAttributes(attrib.cusType, fieldValue)...)
		} else {
			if isArrayValue(fieldValue) {
				attrib.MaxNoOfArrayElements = 1
			}
			if fieldValue.IsValid() {
				attrib.Value = fieldValue.Interface()
			}

			output = append(output, attrib)
		}
	}
	return output
}

func isArrayValue(val interface{}) bool {
	tyVal := reflect.TypeOf(val)
	if tyVal == nil {
		return false
	}
	for tyVal.Kind() == reflect.Ptr {
		tyVal = tyVal.Elem()
	}
	if tyVal != tyBytes && (tyVal.Kind() == reflect.Array || tyVal.Kind() == reflect.Slice) {
		return true
	}
	return false
}
func decodeObject(conn *Connection, parent *ParameterInfo, temporaryLobs *[][]byte) error {
	session := conn.session
	if parent.parent == nil {
		newState := network.SessionState{InBuffer: parent.BValue}
		session.SaveState(&newState)
		defer session.LoadState()
		objectType, err := session.GetByte()
		if err != nil {
			return err
		}
		ctl, err := session.GetInt(4, true, true)
		if err != nil {
			return err
		}
		if ctl == 0xFE {
			_, err = session.GetInt(4, false, true)
			if err != nil {
				return err
			}
		}
		switch objectType {
		case 0x88:
			_ /*attribsLen*/, err := session.GetInt(2, true, true)
			if err != nil {
				return err
			}

			itemsLen, err := session.GetInt(2, false, true)
			if err != nil {
				return err
			}
			if itemsLen == 0xFE {
				itemsLen, err = session.GetInt(4, false, true)
				if err != nil {
					return err
				}
			}
			pars := make([]ParameterInfo, 0, itemsLen)
			for x := 0; x < itemsLen; x++ {
				var tempPar = parent.cusType.attribs[0]
				//if parent.cusType.isRegularArray() {
				//
				//} else {
				//	tempPar = parent.clone()
				//}

				tempPar.Direction = parent.Direction
				if tempPar.DataType == XMLType {
					ctlByte, err := session.GetByte()
					if err != nil {
						return err
					}
					var objectBufferSize int
					if ctlByte == 0xFE {
						objectBufferSize, err = session.GetInt(4, false, true)
						if err != nil {
							return err
						}
					} else {
						objectBufferSize = int(ctlByte)
					}
					tempPar.BValue, err = session.GetBytes(objectBufferSize)
					if err != nil {
						return err
					}
					err = decodeObject(conn, &tempPar, temporaryLobs)
				} else {
					err = tempPar.decodePrimValue(conn, temporaryLobs, true)
				}
				if err != nil {
					return err
				}
				pars = append(pars, tempPar)
			}
			parent.oPrimValue = pars
		case 0x84:
			//pars := make([]ParameterInfo, 0, len(parent.cusType.attribs))
			// collect all attributes in one list
			//pars := getUDTAttributes(parent.cusType, reflect.Value{})
			pars := make([]ParameterInfo, 0, 10)
			for _, attrib := range parent.cusType.attribs {
				attrib.Direction = parent.Direction
				attrib.parent = parent
				// check if this an object or array and comming value is nil
				if attrib.DataType == XMLType {
					temp, err := session.Peek(1)
					if err != nil {
						return err

					}
					if temp[0] == 0xFD || temp[0] == 0xFF {
						_, err = session.GetByte()
						if err != nil {
							return err
						}
					} else {
						if attrib.cusType.isArray {
							attrib.parent = nil
							nb, err := session.GetByte()
							if err != nil {
								return err
							}
							var size int
							switch nb {
							case 0:
								size = 0
							case 0xFE:
								size, err = session.GetInt(4, false, true)
								if err != nil {
									return err
								}
							default:
								size = int(nb)
							}
							if size > 0 {
								attrib.BValue, err = session.GetBytes(size)
								if err != nil {
									return err
								}
							}
						}
						err = decodeObject(conn, &attrib, temporaryLobs)
						if err != nil {
							return err
						}
					}
				} else {
					err := attrib.decodePrimValue(conn, temporaryLobs, true)
					if err != nil {
						return err
					}
				}
				//err = attrib.decodePrimValue(conn, temporaryLobs, true)
				//if err != nil {
				//	return err
				//}
				pars = append(pars, attrib)
			}
			parent.oPrimValue = pars
			//for index, _ := range pars {
			//	pars[index].Direction = parent.Direction
			//	pars[index].parent = parent
			//	// if we get 0xFD this means null object
			//	err = pars[index].decodePrimValue(conn, temporaryLobs, true)
			//	if err != nil {
			//		return err
			//	}
			//}
			// fill pars in its place in sub types
			//parent.oPrimValue, _ = putUDTAttributes(parent.cusType, pars, 0)
		}
	} else {
		pars := make([]ParameterInfo, 0, 10)
		for _, attrib := range parent.cusType.attribs {
			attrib.Direction = parent.Direction
			attrib.parent = parent
			// check if this an object or array and comming value is nil
			if attrib.DataType == XMLType {
				temp, err := session.Peek(1)
				if err != nil {
					return err

				}
				if temp[0] == 0xFD || temp[0] == 0xFF {
					_, err = session.GetByte()
					if err != nil {
						return err
					}
				} else {
					if attrib.cusType.isArray {
						attrib.parent = nil
						nb, err := session.GetByte()
						if err != nil {
							return err
						}
						var size int
						switch nb {
						case 0:
							size = 0
						case 0xFE:
							size, err = session.GetInt(4, false, true)
							if err != nil {
								return err
							}
						default:
							size = int(nb)
						}
						if size > 0 {
							attrib.BValue, err = session.GetBytes(size)
							if err != nil {
								return err
							}
						}
					}
					err = decodeObject(conn, &attrib, temporaryLobs)
					if err != nil {
						return err
					}
				}
			} else {
				err := attrib.decodePrimValue(conn, temporaryLobs, true)
				if err != nil {
					return err
				}
			}

			pars = append(pars, attrib)
		}
		parent.oPrimValue = pars
	}

	return nil
}

func parseInputField(structValue reflect.Value, name, _type string, fieldIndex int) (tempPar *ParameterInfo, err error) {
	tempPar = &ParameterInfo{
		Name:      name,
		Direction: Input,
	}
	fieldValue := structValue.Field(fieldIndex)
	for fieldValue.Type().Kind() == reflect.Ptr {
		if fieldValue.IsNil() {
			tempPar.Value = nil
			return
		}
		fieldValue = fieldValue.Elem()
	}
	if !fieldValue.IsValid() {
		tempPar.Value = nil
		return
	}
	if fieldValue.CanInterface() && fieldValue.Interface() == nil {
		tempPar.Value = nil
		return
	}
	typeErr := fmt.Errorf("error passing field %s as type %s", fieldValue.Type().Name(), _type)
	switch _type {
	case "number":
		//var fieldVal float64
		tempPar.Value, err = NewNumber(fieldValue.Interface()) //getFloat(fieldValue.Interface())
		if err != nil {
			err = typeErr
			return
		}
	case "varchar":
		tempPar.Value = getString(fieldValue.Interface())
	case "nvarchar":
		tempPar.Value = NVarChar(getString(fieldValue.Interface()))
	case "date":
		tempPar.Value, err = getDate(fieldValue.Interface())
		if err != nil {
			err = typeErr
			return
		}
	case "timestamp":
		var fieldVal time.Time
		fieldVal, err = getDate(fieldValue.Interface())
		if err != nil {
			err = typeErr
			return
		}
		tempPar.Value = TimeStamp(fieldVal)
	case "timestamptz":
		var fieldVal time.Time
		fieldVal, err = getDate(fieldValue.Interface())
		if err != nil {
			err = typeErr
			return
		}
		tempPar.Value = TimeStampTZ(fieldVal)
	case "raw":
		tempPar.Value, err = getBytes(fieldValue.Interface())
		if err != nil {
			err = typeErr
			return
		}
	case "clob":
		fieldVal := getString(fieldValue.Interface())
		if len(fieldVal) == 0 {
			tempPar.Value = Clob{Valid: false}
		} else {
			tempPar.Value = Clob{String: fieldVal, Valid: true}
		}
	case "nclob":
		fieldVal := getString(fieldValue.Interface())
		if len(fieldVal) == 0 {
			tempPar.Value = NClob{Valid: false}
		} else {
			tempPar.Value = NClob{String: fieldVal, Valid: true}
		}
	case "blob":
		var fieldVal []byte
		fieldVal, err = getBytes(fieldValue.Interface())
		if err != nil {
			err = typeErr
			return
		}
		tempPar.Value = Blob{Data: fieldVal}
	case "":
		tempPar.Value = fieldValue.Interface()
	default:
		err = typeErr
	}
	return
}
