package fit

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/tormoder/fit/internal/types"
	"io"
	"io/ioutil"
	"log"
	"math"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type MessageHeader struct {
	IsDefinition       bool
	IsDeveloperData    bool
	LocalMessageNumber byte
	TimeOffset         byte
}

func NoneZeroBool(value byte) bool {
	if value != 0 {
		return true
	}
	return false
}

func myDecoder(filepath string){
	//data, _ := ioutil.ReadFile("./testdata/0000a2aa-adc3-4389-8bea-97d52898a2fa.fit")
	//data, _ := ioutil.ReadFile("./testdata/000096E2-9757-4A45-9A6B-7BB2DCCAA96E.fit")
	//data, _ := ioutil.ReadFile("./testdata/1631250047.fit")
	var defmsgs [maxLocalMesgs]*defmsg

	data, _ := ioutil.ReadFile(filepath)
	var size byte
	var tmp [255 * 3]byte
	reader := bytes.NewReader(data)
	err := binary.Read(reader, binary.LittleEndian, &size)
	if err != nil {
		log.Println(err)
	}
	io.ReadFull(reader, tmp[:size-1])
	fileFormat := string(tmp[:size-1])
	fitFormat := fileFormat[len(fileFormat)-5 : len(fileFormat)-2]
	if fitFormat != "FIT" {
		panic("invalid file format")
	}
	_ = checkProtocolVersion(tmp[0])
	log.Println("ProtocolVersion: ", tmp[0])
	log.Println("ProfileVersion: ", binary.LittleEndian.Uint16(tmp[1:3]))
	dataSize := binary.LittleEndian.Uint32(tmp[3:7])
	log.Println("Data Size: ", binary.LittleEndian.Uint32(tmp[3:7]))
	if string(tmp[7:11]) != ".FIT" {
		panic("invalid file format")
	}

	counter := 0
	for {
		//log.Println("size left: ", dataSize, totalSize)
		counter += 1
		if dataSize <= 0 {
			log.Println("DONE")
			break
		}
		io.ReadFull(reader, tmp[:1])
		header := tmp[0]
		dataSize -= 1
		// not (compressed timestamp && is_definition)
		var messageHeader MessageHeader
		if header&0x80 != 0 { // bit 7: Is this record a compressed timestamp?
			messageHeader = MessageHeader{
				IsDefinition:       false,
				IsDeveloperData:    false,
				LocalMessageNumber: (header >> 5) & 0x3,
				TimeOffset:         header & 0x1F,
			}
		} else {
			messageHeader = MessageHeader{
				IsDefinition:       NoneZeroBool(header & 0x40),
				IsDeveloperData:    NoneZeroBool(header & 0x20),
				LocalMessageNumber: header & 0xF,
				TimeOffset:         byte(0),
			}
		}

		if messageHeader.IsDefinition {
			io.ReadFull(reader, tmp[:2])
			dataSize -= 2
			endian := tmp[1]
			dm := defmsg{}
			dm.localMsgType = messageHeader.LocalMessageNumber
			if endian == 0 {
				dm.arch = binary.LittleEndian
			} else {
				dm.arch = binary.BigEndian
			}

			io.ReadFull(reader, tmp[:2])
			dataSize -= 2
			dm.globalMsgNum = MesgNum(dm.arch.Uint16(tmp[:2]))
			io.ReadFull(reader, tmp[:1])
			dataSize -= 1
			dm.fields = tmp[:1][0]
			dm.fieldDefs = make([]fieldDef, dm.fields)
			_, err := io.ReadFull(reader, tmp[0:3*uint16(dm.fields)])
			dataSize -= 3 * uint32(dm.fields)
			if err != nil {
				panic(err)
			}
			for i, fd := range dm.fieldDefs {
				fd.num = tmp[i*3]
				fd.size = tmp[(i*3)+1]
				fd.btype = types.DecodeBase(tmp[(i*3)+2])
				// TODO validate field definition
				//if err = d.validateFieldDef(dm.globalMsgNum, fd); err != nil {
				//
				//}
				dm.fieldDefs[i] = fd
			}

			if messageHeader.IsDeveloperData {
				//开发者字段
				io.ReadFull(reader, tmp[:1])
				dataSize -= 1
				numDevFields := tmp[:1][0]
				dm.fieldDevDefs = make([]fieldDevDef, numDevFields)
				for i := 0; i < int(numDevFields); i++ {
					io.ReadFull(reader, tmp[:3])
					dataSize -= 3
					dm.fieldDevDefs[i] = fieldDevDef{
						num:          tmp[0],
						size:         tmp[1],
						devDataIndex: tmp[2],
						btype:        types.DecodeBase(tmp[(i*3)+2]),
					}
					log.Println("developer Data",
						"field_def_num",
						"field_size",
						"dev_data_index", tmp[0], tmp[1], tmp[2])
				}
			}

			defmsgs[dm.localMsgType] = &dm
		} else {
			dm := defmsgs[messageHeader.LocalMessageNumber]
			if dm == nil {
				panic("no definition messages")
			}
			var msgv reflect.Value
			knownMsg := knownMsgNums[dm.globalMsgNum]
			if knownMsg {
				msgv = getMesgAllInvalid(dm.globalMsgNum)
			}

			for _, dfield := range dm.fieldDefs {
				pfield, _ := getField(dm.globalMsgNum, dfield.num)
				fieldv := msgv.Field(pfield.sindex)

				dsize := int(dfield.size)
				io.ReadFull(reader, tmp[0:dsize])
				dataSize -= uint32(dsize)
				switch pfield.t.Kind() {
				case types.NativeFit:
					if !pfield.t.Array() {
						parseFitField(dm, dfield, tmp[0:dsize], dsize, fieldv)
					} else {

					}
				case types.TimeUTC:
					u32 := dm.arch.Uint32(tmp[:types.BaseUint32.Size()])
					datetime := timeBase.Add(time.Duration(u32) * time.Second)
					fieldv.Set(reflect.ValueOf(datetime))
				case types.TimeLocal:
					u32 := dm.arch.Uint32(tmp[:types.BaseUint32.Size()])
					datetime := timeBase.Add(time.Duration(u32) * time.Second)
					fieldv.Set(reflect.ValueOf(datetime))
				case types.Lat:
					i32 := dm.arch.Uint32(tmp[:types.BaseSint32.Size()])
					lat := NewLatitude(int32(i32))
					fieldv.Set(reflect.ValueOf(lat))
				case types.Lng:
					i32 := dm.arch.Uint32(tmp[:types.BaseSint32.Size()])
					lng := NewLongitude(int32(i32))
					fieldv.Set(reflect.ValueOf(lng))
				default:
					panic("parseDataFields: unreachable: unknown kind")
				}
			}
			PrintValue(msgv)
		}
	}
}

func TestBatchReaderFitFile(t *testing.T) {
	path := "/Users/xingzhe/project/xingzhe/golang/sprinter-go/volumes/fits/"
	dir, _ := ioutil.ReadDir(path)
	for idx, fi := range dir {
		log.Println(idx, filepath.Ext(path + fi.Name()))
		if filepath.Ext(path + fi.Name()) == ".fit"{
			myDecoder(path + fi.Name())
		}
	}
}

func TestReadFitFile(t *testing.T){
	data, _ := ioutil.ReadFile("./testdata/0000a2aa-adc3-4389-8bea-97d52898a2fa.fit")
	reader := bytes.NewReader(data)
	tmp := make([]byte , 3*255)
	parseFileHeader(reader, tmp)
}

func parseFileHeader(reader io.Reader, tmp []byte){
	var size byte
	err := binary.Read(reader, binary.LittleEndian, &size)
	if err != nil {
		log.Println(err)
	}
	io.ReadFull(reader, tmp[:size-1])
	_ = checkProtocolVersion(tmp[0])
	log.Println("ProtocolVersion: ", tmp[0])
	log.Println("ProfileVersion: ", binary.LittleEndian.Uint16(tmp[1:3]))
	dataSize := binary.LittleEndian.Uint32(tmp[3:7])
	log.Println("Data Size: ", dataSize)
	if string(tmp[7:11]) != ".FIT" {
		panic("invalid file format")
	}
}

func parseFitFieldArray() {

}

func parseFitField(dm *defmsg, dfield fieldDef, tmp []byte, dsize int, fieldv reflect.Value) {
	switch dfield.btype {
	case types.BaseByte, types.BaseEnum, types.BaseUint8, types.BaseUint8z:
		fieldv.SetUint(uint64(tmp[0]))
	case types.BaseSint8:
		fieldv.SetInt(int64(tmp[0]))
	case types.BaseSint16:
		fieldv.SetInt(int64(dm.arch.Uint16(tmp[0:dsize])))
	case types.BaseUint16, types.BaseUint16z:
		fieldv.SetUint(uint64(dm.arch.Uint16(tmp[0:dsize])))
	case types.BaseSint32:
		fieldv.SetInt(int64(dm.arch.Uint32(tmp[0:dsize])))
	case types.BaseUint32, types.BaseUint32z:
		fieldv.SetUint(uint64(dm.arch.Uint32(tmp[0:dsize])))
	case types.BaseFloat32:
		bits := dm.arch.Uint32(tmp[0:dsize])
		f32 := float64(math.Float32frombits(bits))
		fieldv.SetFloat(f32)
	case types.BaseFloat64:
		bits := dm.arch.Uint64(tmp[0:dsize])
		f64 := math.Float64frombits(bits)
		fieldv.SetFloat(f64)
	case types.BaseString:
		fieldv.SetString(string(tmp[0:dsize]))
	default:
		log.Println(fmt.Errorf("unknown base type %d for field %v in definition message %v",
			dfield.btype, dfield, dm))
	}
}

func PrintValue(value reflect.Value) {
	switch value.Interface().(type) {
	case FileIdMsg:
		//log.Println("FileIdMsg", tt)
	case FileCreatorMsg:
		//log.Println("FileCreatorMsg", &tt)
	case TimestampCorrelationMsg:
		//log.Println("TimestampCorrelationMsg", &tt)
	default:
		//log.Println("default", value, reflect.TypeOf(tt))
	}
}
