package fit

import (
	"bytes"
	"encoding/binary"
	"github.com/tormoder/fit/internal/types"
	"io"
	"io/ioutil"
	"log"
	"testing"
)

type MessageHeader struct {
	IsDefinition       bool
	IsDeveloperData    bool
	LocalMessageNumber byte
	TimeOffset         byte
}

func NoneZeroBool(value byte) bool {
	log.Println(value)
	if value != 0 {
		return true
	}
	return false
}
func TestReaderFitFile(t *testing.T){
	var defmsgs [maxLocalMesgs]*defmsg
	data, _ := ioutil.ReadFile("./testdata/0000a2aa-adc3-4389-8bea-97d52898a2fa.fit")
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
		counter += 1
		if counter > 100 {
			break
		}
		if dataSize < 0 {
			break
		}
		io.ReadFull(reader, tmp[:1])
		header := tmp[0]
		dataSize -= 1
		// not (compressed timestamp && is_definition)
		var messageHeader MessageHeader
		if header&0x80 != 0 {   // bit 7: Is this record a compressed timestamp?
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
			_, err := io.ReadFull(reader, tmp[0 : 3*uint16(dm.fields)])
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

			for _, dfield := range dm.fieldDefs {
				dsize := int(dfield.size)
				io.ReadFull(reader, tmp[0:dsize])
				if dfield.btype == types.BaseUint32{
					log.Println(dm.arch.Uint32(tmp[0:dsize]), "come on 1")
				} else if dfield.btype == types.BaseUint16{
					log.Println(dm.arch.Uint16(tmp[0:dsize]), "come on 2")
				}else if dfield.btype == types.BaseUint8{
					log.Println(dm.arch.Uint16(tmp[0:dsize]), "come on 3")
				}else if dfield.btype == types.BaseString {
					log.Println(string(tmp[0:dsize]), "come on 4")
				}
			}
		}
	}
}
