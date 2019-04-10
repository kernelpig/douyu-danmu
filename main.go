package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
	"unsafe"

	log "github.com/sirupsen/logrus"
)

// 斗鱼帧头部
type FrameHeader struct {
	Length int32 // 整体长度
}

// 斗鱼消息头部
type PackageHeader struct {
	Length   int32 // 整体长度
	MsgType  int16 // 消息类型
	Encrypt  byte  // 加密方式，暂时为0
	Reserved byte  // 保留字段
}

// 斗鱼消息格式
type Package struct {
	FrameHeader
	PackageHeader
	Msg []byte // 消息内容，以\0结尾
}

// 消息封包，字段为小端
func (p *Package) Pack(writer io.Writer) error {
	binary.Write(writer, binary.LittleEndian, p.FrameHeader.Length)
	binary.Write(writer, binary.LittleEndian, p.PackageHeader.Length)
	binary.Write(writer, binary.LittleEndian, p.PackageHeader.MsgType)
	binary.Write(writer, binary.LittleEndian, p.PackageHeader.Encrypt)
	binary.Write(writer, binary.LittleEndian, p.PackageHeader.Reserved)
	binary.Write(writer, binary.LittleEndian, p.Msg)
	return nil
}

const (
	SERVER_ADDR         = "openbarrage.douyutv.com:8601"
	MSG_TYPE_SEND       = 689
	MSG_TYPE_RECV       = 690
	RESP_TYPE_LOGIN_RES = "loginres" // 登录响应消息
	RESP_TYPE_CHAT_MSG  = "chatmsg"  // 弹幕消息
	RESP_TYPE_DGB       = "dgb"      // 赠送礼物
	RESP_TYPE_UENTER    = "uenter"   // 用户进入房间
	RESP_TYPE_SSD       = "ssd"      // 超级弹幕
	// 其他响应类型
)

// 消息处理器
var MsgHandlerMap = map[string]func(net.Conn, map[string]string){
	RESP_TYPE_LOGIN_RES: loginRespHandler,
	RESP_TYPE_CHAT_MSG:  chatMsgHandler,
	RESP_TYPE_DGB:       dgbRespHandler,
	RESP_TYPE_UENTER:    uenterRespHandler,
	RESP_TYPE_SSD:       ssdRespHandler,
	// 其他响应类型
}

// 默认的响应处理
func defaultRespHandler(conn net.Conn, msgMap map[string]string) {
	msgJson, _ := json.Marshal(msgMap)
	log.Infof("default msg handler: %s\n", string(msgJson))
}

// 超级弹幕
func ssdRespHandler(conn net.Conn, msgMap map[string]string) {
	log.Infof("[超级弹幕][%s][%s]%s\n", msgMap["uid"], msgMap["nn"], msgMap["content"])
}

// 用户进入
func uenterRespHandler(conn net.Conn, msgMap map[string]string) {
	log.Infof("[进入][%s][%s]贵族等级 %s\n", msgMap["uid"], msgMap["nn"], msgMap["nl"])
}

// 赠送礼物消息
func dgbRespHandler(conn net.Conn, msgMap map[string]string) {
	log.Infof("[礼物][%s][%s]\n", msgMap["uid"], msgMap["nn"])
}

// 登录响应处理函数，发送入组消息
func loginRespHandler(conn net.Conn, msgMap map[string]string) {
	log.Infof("login ok")
	groupMsg := fmt.Sprintf("type@=joingroup/rid@=%s/gid@=-9999/", "6523671")
	writer(conn, groupMsg)
}

// 弹幕消息处理
func chatMsgHandler(conn net.Conn, msgMap map[string]string) {
	log.Infof("[弹幕][%s][%s]: %s\n", msgMap["uid"], msgMap["nn"], msgMap["txt"])
}

// 收消息并处理
func reader(conn net.Conn, wg *sync.WaitGroup) {
	for {
		buffer := make([]byte, 1024)
		n, err := conn.Read(buffer)
		if err != nil {
			log.Error("read message err", err)
			continue
		}
		if n == 0 {
			continue
		}
		log.Debug("received buf: ", n, buffer)
		headerSize := int(unsafe.Sizeof(PackageHeader{}) + unsafe.Sizeof(FrameHeader{}))
		if headerSize > n {
			log.Warn("not full msg")
			continue
		}
		buffer = buffer[headerSize:n] // 取出消息内容
		log.Debug("received msg: ", string(buffer))
		pairs := strings.Split(string(buffer), "/")
		msgMap := make(map[string]string)
		for _, v := range pairs {
			subPairs := strings.Split(v, "@=")
			if len(subPairs) == 2 {
				msgMap[subPairs[0]] = subPairs[1]
			}
		}
		msgType, _ := msgMap["type"]
		msgHandler, ok := MsgHandlerMap[msgType]
		if !ok {
			msgHandler = defaultRespHandler
		}
		msgHandler(conn, msgMap)
	}
	wg.Done()
}

// 发送消息处理
func writer(conn net.Conn, msg string) {
	pack := &Package{
		FrameHeader: FrameHeader{
			Length: 0,
		},
		PackageHeader: PackageHeader{
			Length:   0,
			MsgType:  MSG_TYPE_SEND,
			Encrypt:  0,
			Reserved: 0,
		},
		Msg: []byte(msg),
	}
	pack.Msg = append(pack.Msg, 0)
	pack.PackageHeader.Length = int32(unsafe.Sizeof(pack.PackageHeader)) + int32(len(pack.Msg))
	pack.FrameHeader.Length = pack.PackageHeader.Length
	buf := new(bytes.Buffer)
	pack.Pack(buf)
	conn.Write(buf.Bytes())
}

// 发送心跳
func heart(conn net.Conn, wg *sync.WaitGroup) {
	c := time.Tick(15 * time.Second)
	for _ = range c {
		log.Info("send heart")
		writer(conn, "type@=mrkl/")
	}
	wg.Done()
}

// 等待所有go routine
var wg sync.WaitGroup

// 初始化函数
func init() {
	log.SetLevel(log.DebugLevel)
}

func main() {
	conn, err := net.Dial("tcp", SERVER_ADDR)
	if err != nil {
		log.Fatal("connect to server err")
	}
	log.Info("connect to server ok")
	defer conn.Close()

	go heart(conn, &wg)
	go reader(conn, &wg)
	wg.Add(2)

	loginMessage := fmt.Sprintf("type@=loginreq/roomid@=%s/", "6523671")
	writer(conn, loginMessage)

	wg.Wait()
}
