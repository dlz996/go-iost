package v8

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"time"

	. "github.com/golang/mock/gomock"
	"github.com/iost-official/go-iost/core/contract"
	"github.com/iost-official/go-iost/ilog"
	"github.com/iost-official/go-iost/vm/database"
	"github.com/iost-official/go-iost/vm/host"
)

var vmPool *VMPool

func init() {
	vmPool = NewVMPool(3, 3)
	//vmPool.SetJSPath("./v8/libjs/")
	vmPool.Init()
}

var testDataPath = "./test_data/"

func ReadFile(src string) ([]byte, error) {
	fi, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer fi.Close()
	fd, err := ioutil.ReadAll(fi)
	if err != nil {
		return nil, err
	}
	return fd, nil
}

func Init(t *testing.T) *database.Visitor {
	mc := NewController(t)
	defer mc.Finish()
	db := database.NewMockIMultiValue(mc)
	vi := database.NewVisitor(100, db)
	return vi
}

func MyInit(t *testing.T, conName string, optional ...interface{}) (*host.Host, *contract.Contract) {
	db := database.NewDatabaseFromPath(testDataPath + conName + ".json")
	vi := database.NewVisitor(100, db)

	ctx := host.NewContext(nil)
	ctx.Set("gas_price", int64(1))
	var gasLimit = int64(10000)
	if len(optional) > 0 {
		gasLimit = optional[0].(int64)
	}
	ctx.GSet("gas_limit", gasLimit)
	ctx.Set("contract_name", conName)
	h := host.NewHost(ctx, vi, nil, ilog.DefaultLogger())

	fd, err := ReadFile(testDataPath + conName + ".js")
	if err != nil {
		t.Fatal("Read file failed: ", err.Error())
		return nil, nil
	}
	rawCode := string(fd)

	code := &contract.Contract{
		ID:   conName,
		Code: rawCode,
	}

	expTime := time.Now().Add(time.Second * 10)
	h.SetDeadline(expTime)
	code.Code, _ = vmPool.Compile(code)

	return h, code
}

func TestEngine_LoadAndCall(t *testing.T) {
	vi := Init(t)
	ctx := host.NewContext(nil)
	ctx.Set("gas_price", int64(1))
	ctx.GSet("gas_limit", int64(1000000000))
	ctx.Set("contract_name", "contractName")
	tHost := host.NewHost(ctx, vi, nil, nil)
	expTime := time.Now().Add(time.Second * 10)
	tHost.SetDeadline(expTime)

	code := &contract.Contract{
		ID: "test.js",
		Code: `
var Contract = function() {
}

	Contract.prototype = {
	fibonacci: function(cycles) {
			if (cycles == 0) return 0;
			if (cycles == 1) return 1;
			return this.fibonacci(cycles - 1) + this.fibonacci(cycles - 2);
		}
	}

	module.exports = Contract
`,
	}

	rs, _, err := vmPool.LoadAndCall(tHost, code, "fibonacci", "12")

	if err != nil {
		t.Fatalf("LoadAndCall run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0] != "144" {
		t.Fatalf("LoadAndCall except 144, got %s\n", rs[0])
	}
}

func TestEngine_bigNumber(t *testing.T) {
	host, code := MyInit(t, "bignumber1")
	vmPool.LoadAndCall(host, code, "constructor")
	rs, _, err := vmPool.LoadAndCall(host, code, "getVal")

	if err != nil {
		t.Fatalf("LoadAndCall getVal error: %v\n", err)
	}
	if len(rs) != 1 || rs[0].(string) != "\"8.00029e-11\"" {
		t.Errorf("LoadAndCall except 8.00029e-11, got %s\n", rs[0])
	}
}

// nolint
func TestEngine_Storage(t *testing.T) {
	host, code := MyInit(t, "storage1")

	// vmPool.LoadAndCall(host, code, "constructor")
	rs, _, err := vmPool.LoadAndCall(host, code, "get", "a")
	if err != nil {
		t.Fatalf("LoadAndCall get run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0].(string) != "1000" {
		t.Fatalf("LoadAndCall except mySetVal, got %s\n", rs[0])
	}

	rtn, cost, err := vmPool.LoadAndCall(host, code, "put", "mySetKey", "mySetVal")
	if err != nil {
		t.Fatalf("LoadAndCall put run error: %v\n", err)
	}
	if len(rtn) != 1 || rtn[0] != "0" {
		t.Fatalf("return of put should be float64 0")
	}
	t.Log(cost)

	rs, _, err = vmPool.LoadAndCall(host, code, "get", "mySetKey")
	if err != nil {
		t.Fatalf("LoadAndCall get run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0].(string) != "mySetVal" {
		t.Fatalf("LoadAndCall except mySetVal, got %s\n", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "getThisNum")
	if err != nil {
		t.Fatalf("LoadAndCall getThisNum run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0].(string) != "99" {
		t.Fatalf("LoadAndCall except 99, got %s\n", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "getThisStr")
	if err != nil {
		t.Fatalf("LoadAndCall getThisStr run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0].(string) != "yeah" {
		t.Fatalf("LoadAndCall except yeah, got %s\n", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "get", "mySetKeynotfound", "string")
	if err != nil {
		t.Fatalf("LoadAndCall get run error: %v\n", err)
	}
	// todo get return nil
	if len(rs) != 1 || rs[0].(string) != "nil" {
		t.Fatalf("LoadAndCall except mySetVal, got %s\n", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "mhas", "ptable", "a")
	if err != nil {
		t.Fatalf("LoadAndCall mhas run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0].(string) != "false" {
		t.Fatalf("LoadAndCall except false, got %s\n", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "mset", "ptable", "a", "aa")
	if err != nil {
		t.Fatalf("LoadAndCall mset run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0].(string) != "0" {
		t.Fatalf("LoadAndCall except 0, got %s\n", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "mhas", "ptable", "a")
	if err != nil {
		t.Fatalf("LoadAndCall mhas run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0].(string) != "true" {
		t.Fatalf("LoadAndCall except true, got %s\n", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "mget", "ptable", "a")
	if err != nil {
		t.Fatalf("LoadAndCall mget run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0].(string) != "aa" {
		t.Fatalf("LoadAndCall except aa, got %s\n", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "mdelete", "ptable", "a")
	if err != nil {
		t.Fatalf("LoadAndCall mdelete run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0].(string) != "0" {
		t.Fatalf("LoadAndCall except 0, got %s\n", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "mhas", "ptable", "a")
	if err != nil {
		t.Fatalf("LoadAndCall mhas run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0].(string) != "false" {
		t.Fatalf("LoadAndCall except true, got %s\n", rs[0])
	}
}

// nolint
func TestEngine_DataType(t *testing.T) {
	host, code := MyInit(t, "datatype")

	rs, _, err := vmPool.LoadAndCall(host, code, "number", 1)
	if err != nil {
		t.Fatalf("LoadAndCall number run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0] != "0.5555555555" {
		t.Fatalf("LoadAndCall except 0.5555555555, got %s\n", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "number_big", 1)
	if err != nil {
		t.Fatalf("LoadAndCall number_big run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0] != "0.5555555555" {
		t.Fatalf("LoadAndCall except 0.5555555555, got %s\n", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "number_op")
	if err != nil {
		t.Fatalf("LoadAndCall number_op run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0] != "3" {
		t.Fatalf("LoadAndCall except 3, got %s\n", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "number_op2")
	if err != nil {
		t.Fatalf("LoadAndCall number_op2 run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0] != "2" {
		t.Fatalf("LoadAndCall except 2, got %s\n", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "number_strange")
	if err != nil {
		t.Fatalf("LoadAndCall number_strange run error: %v\n", err)
	}
	// todo get return string -infinity
	if len(rs) != 1 || rs[0].(string) != "-Infinity" {
		t.Fatalf("LoadAndCall except Infinity, got %s\n", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "param", 2)
	if err != nil {
		t.Fatalf("LoadAndCall param run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0].(string) != "4" {
		t.Fatalf("LoadAndCall except 4, got %s\n", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "param2")
	if err != nil {
		t.Fatalf("LoadAndCall param run error: %v\n", err)
	}
	// todo get return string undefined
	if len(rs) != 1 || rs[0] != "null" {
		t.Fatalf("LoadAndCall except undefined, got %s\n", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "bool")
	if err != nil {
		t.Fatalf("LoadAndCall bool run error: %v\n", err)
	}
	// todo get return string false
	if len(rs) != 1 || rs[0].(string) != "false" {
		t.Fatalf("LoadAndCall except undefined, got %s\n", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "string")
	if err != nil {
		t.Fatalf("LoadAndCall string run error: %v\n", err)
	}
	if len(rs) != 1 || len(rs[0].(string)) != 4096 {
		t.Fatalf("LoadAndCall except len 4096, got %d\n", len(rs[0].(string)))
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "array")
	if err != nil {
		t.Fatalf("LoadAndCall array run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0].(string) != "[0,1,2,3]" {
		t.Fatalf("LoadAndCall except [0,1,2,3], got %s\n", rs[0].(string))
	}

	// todo get return string [object Object]
	/*
		rs, _,err = e.LoadAndCall(host, code, "object")
		if err != nil {
			t.Fatalf("LoadAndCall array run error: %v\n", err)
		}
		if len(rs) != 1 || rs[0].(string) != "0,1,2,3"{
			t.Errorf("LoadAndCall except 0,1,2,3, got %s\n", rs[0].(string))
		}
	*/
}

// nolint
func TestEngine_Loop(t *testing.T) {
	t.Skip()
	host, code := MyInit(t, "loop")

	_, _, err := vmPool.LoadAndCall(host, code, "for")
	if err == nil || err.Error() != "out of gas" {
		t.Fatalf("LoadAndCall for should return error: out of gas, but got %v\n", err)
	}

	host, code = MyInit(t, "loop")
	_, _, err = vmPool.LoadAndCall(host, code, "for2")
	if err == nil || err.Error() != "out of gas" {
		t.Fatalf("LoadAndCall for should return error: out of gas, but got %v\n", err)
	}

	host, code = MyInit(t, "loop")
	rs, _, err := vmPool.LoadAndCall(host, code, "for3")
	if err != nil {
		t.Fatalf("LoadAndCall for3 run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0].(string) != "10" {
		t.Fatalf("LoadAndCall except 10, got %s\n", rs[0].(string))
	}

	host, code = MyInit(t, "loop")
	rs, _, err = vmPool.LoadAndCall(host, code, "forin")
	if err != nil {
		t.Fatalf("LoadAndCall forin run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0].(string) != "12" {
		t.Fatalf("LoadAndCall except 12, got %s\n", rs[0].(string))
	}

	host, code = MyInit(t, "loop")
	rs, _, err = vmPool.LoadAndCall(host, code, "forof")
	if err != nil {
		t.Fatalf("LoadAndCall forof run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0].(string) != "6" {
		t.Fatalf("LoadAndCall except 6, got %s\n", rs[0].(string))
	}

	host, code = MyInit(t, "loop")
	_, _, err = vmPool.LoadAndCall(host, code, "while")
	if err == nil || err.Error() != "out of gas" {
		t.Fatalf("LoadAndCall for should return error: out of gas, but got %v\n", err)
	}

	host, code = MyInit(t, "loop")
	_, _, err = vmPool.LoadAndCall(host, code, "dowhile")
	if err == nil || err.Error() != "out of gas" {
		t.Fatalf("LoadAndCall for should return error: out of gas, but got %v\n", err)
	}
}

func TestEngine_Func(t *testing.T) {
	// Please @shiqi fix it
	t.SkipNow()
	host, code := MyInit(t, "func")
	_, _, err := vmPool.LoadAndCall(host, code, "func1")
	if err == nil || err.Error() != "out of gas" {
		t.Fatalf("LoadAndCall for should return error: out of gas, but got %v\n", err)
	}

	//host, code = MyInit(t, "func", int64(100000000000))
	//_, _, err = vmPool.LoadAndCall(host, code, "func1")
	//if err == nil || !strings.Contains(err.Error(), "Uncaught exception: RangeError: Maximum call stack size exceeded") {
	//	t.Fatalf("LoadAndCall for should return error: Uncaught exception: RangeError: Maximum call stack size exceeded, but got %v\n", err)
	//}

	host, code = MyInit(t, "func")
	rs, _, err := vmPool.LoadAndCall(host, code, "func3", 4)
	if err != nil {
		t.Fatalf("LoadAndCall func3 run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0].(string) != "9" {
		t.Fatalf("LoadAndCall except 9, got %s\n", rs[0].(string))
	}

	host, code = MyInit(t, "func")
	rs, _, err = vmPool.LoadAndCall(host, code, "func4")
	if err != nil {
		t.Fatalf("LoadAndCall func4 run error: %v\n", err)
	}
	if len(rs) != 1 || rs[0].(string) != "[2,5,5]" {
		t.Fatalf("LoadAndCall except [2,5,5], got %s\n", rs[0].(string))
	}
}

func TestEngine_Danger(t *testing.T) {
	host, code := MyInit(t, "danger")
	/*
		_, _, err := vmPool.LoadAndCall(host, code, "bigArray")
		if err != nil {
			t.Fatal("LoadAndCall for should return no error, got %s", err.Error())
		}
	*/

	_, _, err := vmPool.LoadAndCall(host, code, "visitUndefined")
	if err == nil || !strings.Contains(err.Error(), "Uncaught exception: TypeError: Cannot set property 'c' of undefined") {
		t.Fatalf("LoadAndCall for should return error: Uncaught exception: TypeError: Cannot set property 'c' of undefined, but got %v\n", err)
	}

	host, code = MyInit(t, "danger")
	_, _, err = vmPool.LoadAndCall(host, code, "throw")
	if err == nil || !strings.Contains(err.Error(), "Uncaught exception: test throw") {
		t.Fatalf("LoadAndCall for should return error: Uncaught exception: test throw, but got %v\n", err)
	}
}

// nolint
func TestEngine_Int64(t *testing.T) {
	host, code := MyInit(t, "int64Test")
	rs, _, err := vmPool.LoadAndCall(host, code, "getPlus")
	if err != nil {
		t.Fatalf("LoadAndCall getPlus error: %v", err)
	}
	if len(rs) > 0 && rs[0] != "1234501234" {
		t.Fatalf("LoadAndCall getPlus except: , got: %v", rs[0])
	}
	rs, _, err = vmPool.LoadAndCall(host, code, "getMinus")
	if err != nil {
		t.Fatalf("LoadAndCall getMinus error: %v", err)
	}
	if len(rs) > 0 && rs[0] != "123400109" {
		t.Fatalf("LoadAndCall getMinus except: , got: %v", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "getMulti")
	if err != nil {
		t.Fatalf("LoadAndCall getMulti error: %v", err)
	}
	if len(rs) > 0 && rs[0] != "148148004" {
		t.Fatalf("LoadAndCall getMulti except: , got: %v", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "getDiv")
	if err != nil {
		t.Fatalf("LoadAndCall getDiv error: %v", err)
	}
	if len(rs) > 0 && rs[0] != "1028805" {
		t.Fatalf("LoadAndCall getDiv except: , got: %v", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "getMod")
	if err != nil {
		t.Fatalf("LoadAndCall getMod error: %v", err)
	}
	if len(rs) > 0 && rs[0] != "7" {
		t.Fatalf("LoadAndCall getMod except: , got: %v", rs[0])
	}

	rs, _, err = vmPool.LoadAndCall(host, code, "getPow", 3)
	if err != nil {
		t.Fatalf("LoadAndCall getPow error: %v", err)
	}
	if len(rs) > 0 && rs[0] != "1879080904" {
		t.Fatalf("LoadAndCall getPow except: 1879080904, got: %v", rs[0])
	}
}

func TestEngine_Console(t *testing.T) {
	host, code := MyInit(t, "console1")
	_, _, err := vmPool.LoadAndCall(host, code, "log")
	if err != nil {
		t.Fatalf("LoadAndCall console error: %v", err)
	}
}

func TestEngine_Blockchain(t *testing.T) {
	host, code := MyInit(t, "blockchain1")
	rs, _, err := vmPool.LoadAndCall(host, code, "gs")
	if err != nil {
		t.Fatalf("LoadAndCall console error: %v", err)
	}
	t.Log(rs)
}
