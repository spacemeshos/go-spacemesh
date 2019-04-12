// Package xdr provides helper types and methods for XDR-based encoding and decoding.
package xdr

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"testing"
)

var (
	reader io.Reader = &encodableReader{1, 2}
)

type testEncoder struct {
	err error
}

type encodableReader struct {
	A, B uint
}

func (e *encodableReader) Read(b []byte) (int, error) {
	panic("called")
}

// encTest represents a mock encoding value.
type encTest struct {
	val           interface{} // Value
	output, error string      // Output
}

type byteEncoder byte

type namedByteType byte

type simplestruct struct {
	A uint
	B string
}

type recstruct struct {
	I     uint
	Child *recstruct
}

type tailRaw struct {
	A    uint
	Tail []RawValue
}

type hasIgnoredField struct {
	A uint
	B uint
	C uint
}

// unhex decodes a byte slice form a given hex string.
func unhex(str string) []byte {
	b, err := hex.DecodeString(strings.Replace(str, " ", "", -1)) // Decode

	if err != nil { // Check for errors
		panic(fmt.Sprintf("invalid hex string: %q", str)) // Panic
	}

	return b // Return decoded
}

// encTests represents xdr unit testing encodable values.
var encTests = []encTest{
	// booleans
	{val: true, output: "00000001"},
	{val: false, output: "00000000"},

	// integers
	{val: uint32(0), output: "00000000"},
	{val: uint32(127), output: "0000007F"},
	{val: uint32(128), output: "00000080"},
	{val: uint32(256), output: "00000100"},
	{val: uint32(1024), output: "00000400"},
	{val: uint32(0xFFFFFF), output: "00FFFFFF"},
	{val: uint32(0xFFFFFFFF), output: "FFFFFFFF"},
	{val: uint64(0xFFFFFFFF), output: "00000000FFFFFFFF"},
	{val: uint64(0xFFFFFFFFFF), output: "000000FFFFFFFFFF"},
	{val: uint64(0xFFFFFFFFFFFF), output: "0000FFFFFFFFFFFF"},
	{val: uint64(0xFFFFFFFFFFFFFF), output: "00FFFFFFFFFFFFFF"},
	{val: uint64(0xFFFFFFFFFFFFFFFF), output: "FFFFFFFFFFFFFFFF"},

	// big integers (should match uint for small values)
	{val: big.NewInt(0), output: "00000000"},
	{val: big.NewInt(1), output: "0000000101000000"},
	{val: big.NewInt(127), output: "000000017F000000"},
	{val: big.NewInt(128), output: "0000000180000000"},
	{val: big.NewInt(256), output: "0000000201000000"},
	{val: big.NewInt(1024), output: "0000000204000000"},
	{val: big.NewInt(0xFFFFFF), output: "00000003FFFFFF00"},
	{val: big.NewInt(0xFFFFFFFF), output: "00000004FFFFFFFF"},
	{val: big.NewInt(0xFFFFFFFFFF), output: "00000005FFFFFFFFFF000000"},
	{val: big.NewInt(0xFFFFFFFFFFFF), output: "00000006FFFFFFFFFFFF0000"},
	{val: big.NewInt(0xFFFFFFFFFFFFFF), output: "00000007FFFFFFFFFFFFFF00"},
	{
		val:    big.NewInt(0).SetBytes(unhex("102030405060708090A0B0C0D0E0F2")),
		output: "0000000F102030405060708090A0B0C0D0E0F200",
	},
	{
		val:    big.NewInt(0).SetBytes(unhex("0100020003000400050006000700080009000A000B000C000D000E01")),
		output: "0000001C0100020003000400050006000700080009000A000B000C000D000E01",
	},
	{
		val:    big.NewInt(0).SetBytes(unhex("010000000000000000000000000000000000000000000000000000000000000000")),
		output: "00000021010000000000000000000000000000000000000000000000000000000000000000000000",
	},

	// non-pointer big.Int
	{val: *big.NewInt(0), output: "00000000"},
	{val: *big.NewInt(0xFFFFFF), output: "00000003FFFFFF00"},

	// byte slices, strings
	{val: []byte{}, output: "00000000"},
	{val: []byte{0x7E}, output: "000000017E000000"},
	{val: []byte{0x7F}, output: "000000017F000000"},
	{val: []byte{0x80}, output: "0000000180000000"},
	{val: []byte{1, 2, 3}, output: "0000000301020300"},

	{val: "", output: "00000000"},
	{val: "\x7E", output: "000000017E000000"},
	{val: "\x7F", output: "000000017F000000"},
	{val: "\x80", output: "0000000180000000"},
	{val: "dog", output: "00000003646F6700"},
	{
		val:    "Lorem ipsum dolor sit amet, consectetur adipisicing eli",
		output: "000000374C6F72656D20697073756D20646F6C6F722073697420616D65742C20636F6E7365637465747572206164697069736963696E6720656C6900",
	},
	{
		val:    "Lorem ipsum dolor sit amet, consectetur adipisicing elit",
		output: "000000384C6F72656D20697073756D20646F6C6F722073697420616D65742C20636F6E7365637465747572206164697069736963696E6720656C6974",
	},
	{
		val:    "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Curabitur mauris magna, suscipit sed vehicula non, iaculis faucibus tortor. Proin suscipit ultricies malesuada. Duis tortor elit, dictum quis tristique eu, ultrices at risus. Morbi a est imperdiet mi ullamcorper aliquet suscipit nec lorem. Aenean quis leo mollis, vulputate elit varius, consequat enim. Nulla ultrices turpis justo, et posuere urna consectetur nec. Proin non convallis metus. Donec tempor ipsum in mauris congue sollicitudin. Vestibulum ante ipsum primis in faucibus orci luctus et ultrices posuere cubilia Curae; Suspendisse convallis sem vel massa faucibus, eget lacinia lacus tempor. Nulla quis ultricies purus. Proin auctor rhoncus nibh condimentum mollis. Aliquam consequat enim at metus luctus, a eleifend purus egestas. Curabitur at nibh metus. Nam bibendum, neque at auctor tristique, lorem libero aliquet arcu, non interdum tellus lectus sit amet eros. Cras rhoncus, metus ac ornare cursus, dolor justo ultrices metus, at ullamcorper volutpat",
		output: "000004004C6F72656D20697073756D20646F6C6F722073697420616D65742C20636F6E73656374657475722061646970697363696E6720656C69742E20437572616269747572206D6175726973206D61676E612C20737573636970697420736564207665686963756C61206E6F6E2C20696163756C697320666175636962757320746F72746F722E2050726F696E20737573636970697420756C74726963696573206D616C6573756164612E204475697320746F72746F7220656C69742C2064696374756D2071756973207472697374697175652065752C20756C7472696365732061742072697375732E204D6F72626920612065737420696D70657264696574206D6920756C6C616D636F7270657220616C6971756574207375736369706974206E6563206C6F72656D2E2041656E65616E2071756973206C656F206D6F6C6C69732C2076756C70757461746520656C6974207661726975732C20636F6E73657175617420656E696D2E204E756C6C6120756C74726963657320747572706973206A7573746F2C20657420706F73756572652075726E6120636F6E7365637465747572206E65632E2050726F696E206E6F6E20636F6E76616C6C6973206D657475732E20446F6E65632074656D706F7220697073756D20696E206D617572697320636F6E67756520736F6C6C696369747564696E2E20566573746962756C756D20616E746520697073756D207072696D697320696E206661756369627573206F726369206C756374757320657420756C74726963657320706F737565726520637562696C69612043757261653B2053757370656E646973736520636F6E76616C6C69732073656D2076656C206D617373612066617563696275732C2065676574206C6163696E6961206C616375732074656D706F722E204E756C6C61207175697320756C747269636965732070757275732E2050726F696E20617563746F722072686F6E637573206E69626820636F6E64696D656E74756D206D6F6C6C69732E20416C697175616D20636F6E73657175617420656E696D206174206D65747573206C75637475732C206120656C656966656E6420707572757320656765737461732E20437572616269747572206174206E696268206D657475732E204E616D20626962656E64756D2C206E6571756520617420617563746F72207472697374697175652C206C6F72656D206C696265726F20616C697175657420617263752C206E6F6E20696E74657264756D2074656C6C7573206C65637475732073697420616D65742065726F732E20437261732072686F6E6375732C206D65747573206163206F726E617265206375727375732C20646F6C6F72206A7573746F20756C747269636573206D657475732C20617420756C6C616D636F7270657220766F6C7574706174",
	},

	// slices
	{val: []uint{}, output: "00000000"},
	{val: []uint{1, 2, 3}, output: "00000003000000010000000200000003"},
	{
		// [ [], [[]], [ [], [[]] ] ]
		val:    []interface{}{[]interface{}{}, [][]interface{}{{}}, []interface{}{[]interface{}{}, [][]interface{}{{}}}},
		output: "0000000300000000000000010000000000000002000000000000000100000000",
	},
	{
		val:    []string{"aaa", "bbb", "ccc", "ddd", "eee", "fff", "ggg", "hhh", "iii", "jjj", "kkk", "lll", "mmm", "nnn", "ooo"},
		output: "0000000F000000036161610000000003626262000000000363636300000000036464640000000003656565000000000366666600000000036767670000000003686868000000000369696900000000036A6A6A00000000036B6B6B00000000036C6C6C00000000036D6D6D00000000036E6E6E00000000036F6F6F00",
	},
	{
		val:    []interface{}{uint(1), uint(0xFFFFFF), []interface{}{[]uint{4, 5, 5}}, "abc"},
		output: "000000040000000100FFFFFF00000001000000030000000400000005000000050000000361626300",
	},
	{
		val: [][]string{
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
			{"asdf", "qwer", "zxcv"},
		},
		output: "000000200000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A7863760000000300000004617364660000000471776572000000047A786376",
	},

	// RawValue
	{val: RawValue(unhex("01")), output: "0000000101000000"},
	{val: RawValue(unhex("82FFFF")), output: "0000000382FFFF00"},
	{val: []RawValue{unhex("01"), unhex("02")}, output: "0000000200000001010000000000000102000000"},

	// structs
	{val: simplestruct{}, output: "0000000000000000"},
	{val: simplestruct{A: 3, B: "foo"}, output: "0000000300000003666F6F00"},
	{val: &recstruct{5, nil}, output: "0000000500000000"},
	{val: &recstruct{5, &recstruct{4, &recstruct{3, nil}}}, output: "000000050000000100000004000000010000000300000000"},
	{val: &tailRaw{A: 1, Tail: []RawValue{unhex("02"), unhex("03")}}, output: "000000010000000200000001020000000000000103000000"},
	{val: &tailRaw{A: 1, Tail: []RawValue{unhex("02")}}, output: "00000001000000010000000102000000"},
	{val: &tailRaw{A: 1, Tail: []RawValue{}}, output: "0000000100000000"},
	{val: &tailRaw{A: 1, Tail: nil}, output: "0000000100000000"},
	{val: &hasIgnoredField{A: 1, B: 2, C: 3}, output: "000000010000000200000003"},

	// nil
	{val: (*uint)(nil), output: "00000000"},
	{val: (*string)(nil), output: "00000000"},
	{val: (*[]byte)(nil), output: "00000000"},
	{val: (*[10]byte)(nil), output: "00000000"},
	{val: (*big.Int)(nil), output: "00000000"},
	{val: (*[]string)(nil), output: "00000000"},
	{val: (*[10]string)(nil), output: "00000000"},
	{val: (*[]interface{})(nil), output: "00000000"},
	{val: (*[]struct{ uint })(nil), output: "00000000"},
	{val: (*interface{})(nil), output: "00000000"},

	// interfaces
	{val: []io.Reader{reader}, output: "000000010000000100000002"}, // the contained value is a struct

	// Encoder
	{val: (*testEncoder)(nil), output: "00000000"},
	{val: &testEncoder{}, output: ""},
	{val: &testEncoder{errors.New("test error")}, output: ""},
	// verify that pointer method testEncoder.EncodeRLP is called for
	// addressable non-pointer values.
	{val: &struct{ TE testEncoder }{testEncoder{}}, output: ""},
	{val: &struct{ TE testEncoder }{testEncoder{errors.New("test error")}}, output: ""},
}

// runEncTests runs all encoder tests.
func runEncTests(t *testing.T, f func(val interface{}) ([]byte, error)) {
	for i, test := range encTests {
		output, err := f(test.val)
		if err != nil && test.error == "" {
			t.Errorf("test %d: unexpected error: %v\nvalue %#v\ntype %T",
				i, err, test.val, test.val)
			continue
		}

		if test.error != "" && fmt.Sprint(err) != test.error {
			t.Errorf("test %d: error mismatch\ngot   %v\nwant  %v\nvalue %#v\ntype  %T",
				i, err, test.error, test.val, test.val)
			continue
		}

		if err == nil && !bytes.Equal(output, unhex(test.output)) {
			t.Errorf("test %d: output mismatch:\ngot   %X\nwant  %s\nvalue %#v\ntype  %T",
				i, output, test.output, test.val, test.val)
		}
	}
}

// TestMarshal tests the functionality of the Marshal() method,
func TestMarshal(t *testing.T) {
	runEncTests(t, func(val interface{}) ([]byte, error) {
		b := new(bytes.Buffer) // Initialize buffer

		_, err := Marshal(b, val) // Encode

		return b.Bytes(), err // Return encoded
	})
}
