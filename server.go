package grlrpc

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"grlrpc/codec"
	"io"
	"log"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
)

const MagicNumber = 0x3bef5c

type Option struct {
	MagicNumber int
	CodecType codec.Type
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GobType,
}

type Server struct {}

func NewServer() *Server  {
	return &Server{}
}

var DefaultServer = NewServer()

func (s * Server) Accept(lis net.Listener)  {
	for  {
		conn, err := lis.Accept()
		if err != nil{
			log.Println("rpc server: accept error:",err)
			return
		}
		go s.ServeConn(conn)
	}
}
func Accept(lis net.Listener)  {
	DefaultServer.Accept(lis)
}

func (s * Server) ServeConn(conn io.ReadWriteCloser)  {
	defer func() {_ = conn.Close()}()
	var opt Option

	if err := json.NewDecoder(conn).Decode(&opt); err != nil{
		log.Println("rpc server : options error : ",err)
		return
	}

	if opt.MagicNumber != MagicNumber{
		log.Print("rpc server : invalid magic number %x",opt.MagicNumber)
		return
	}
	//f是一个函数
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil{
		log.Printf("rpc server : invalid codec type %s",opt.CodecType)
		return
	}

	s.serveCodec(f(conn))
}

var invalidRequest = struct {}{}

func (s * Server) serveCodec(cc codec.Codec)  {
	sending := new(sync.Mutex)

	wg := new(sync.WaitGroup)

	for  {
		req,err := s.readRequest(cc)
		if err != nil{
			if req == nil{
				break
			}
			req.h.Error = err.Error()
			s.sendResponse(cc,req.h,invalidRequest,sending)
			continue
		}
		wg.Add(1)
		go s.handleRequest(cc,req,sending,wg)
	}
	wg.Wait()
	_ = cc.Close()
}

type request struct {
	h *codec.Header
	argv,reply reflect.Value
	
}

func (s * Server) readRequestHeader(cc codec.Codec)  (*codec.Header,error){
	var h codec.Header
	if err := cc.ReadHeader(&h); err != nil{
		if err != io.EOF && err != io.ErrUnexpectedEOF{
			log.Println("rpc server : read header error :",err)
		}
		return nil, err
	}
	return &h,nil
	
}

func (s * Server) readRequest(cc codec.Codec)  (*request,error){
	h,err := s.readRequestHeader(cc)
	if err != nil{
		return nil, err
	}
	req := &request{h: h}

	req.argv = reflect.New(reflect.TypeOf(""))
	if err = cc.ReadBody(req.argv.Interface()); err != nil{
		log.Println("rpc server : read argv err :",err)
	}
	return req,nil
}

func (s * Server) sendResponse(cc codec.Codec,h * codec.Header,body interface{},sending *sync.Mutex)  {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h,body); err != nil{
		log.Println("rpc server : write response error:",err)
	}
}

func (s * Server) handleRequest(cc codec.Codec,req * request,sending * sync.Mutex,wg *sync.WaitGroup)  {
	defer wg.Done()
	log.Println(req.h,req.argv.Elem())
	req.reply = reflect.ValueOf(fmt.Sprintf("grlrpc resp %d",req.h.Seq))
	s.sendResponse(cc,req.h,req.reply.Interface(),sending)

}

type methodType struct {
	method reflect.Method
	ArgType reflect.Type
	ReplyType reflect.Type
	numCalls uint64
}

func (m * methodType) NumCalls() uint64 {
	return atomic.LoadUint64(&m.numCalls)
}

func (m * methodType) newArgv() reflect.Value  {
	var argv reflect.Value
	//指针类型和值类型的创建略有区别
	if m.ArgType.Kind() == reflect.Ptr{
		argv = reflect.New(m.ArgType.Elem())
	}else {
		argv = reflect.New(m.ArgType).Elem()
	}

	return argv
}

func (m * methodType) newReplyv()  reflect.Value{
	replyv := reflect.New(m.ReplyType.Elem())
	switch m.ReplyType.Elem().Kind() {
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(),0,0))
	}

	return replyv

}

type service struct {
	name string
	typ reflect.Type
	rcvr reflect.Value
	method map[string]*methodType

}

func newService(rcvr interface{})  *service{
	s := new(service)
	s.rcvr = reflect.ValueOf(rcvr)
	s.name = reflect.Indirect(s.rcvr).Type().Name()
	s.typ = reflect.TypeOf(rcvr)
	
	if !ast.IsExported(s.name){
		log.Fatalf("rpc server : %s is not a valid service name",s.name)
	}
	//注册方法
}

func (s * service)  registerMethods(){
	s.method = make(map[string]*methodType)
	for i := 0;i < s.typ.NumMethod();i++{
		method := s.typ.Method(i)
		mType := method.Type
		if mType.NumIn() != 3 || mType.NumOut() != 1{
			continue
		}
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem(){
			continue
		}
		argType,replyType := mType.In(1),mType.In(2)
		//
	}
	
}

