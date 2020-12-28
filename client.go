package grlrpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"grlrpc/codec"
	"io"
	"log"
	"net"
	"sync"
)


type Call struct {
	Seq           uint64      //请求号
	ServiceMethod string      // server.method
	Args          interface{} //发送的request
	Reply         interface{} //接收从服务端返回的数据
	Error         error
	Done          chan *Call //一个call类型的管道
}

func (c *Call) done() {
	c.Done <- c // 将
}

type Client struct {
	cc codec.Codec //消息的编码器，用来序列化需要发出去的请求，以及反序列化接收到的响应

	opt *Option //格式选项

	sending sync.Mutex //类似于服务端，为了保证请求的有序发送

	header codec.Header //每个请求的消息头，header只有在请求发送时才需要，而请求发送是互斥的，因此每个客户端只需要一个header

	mu sync.Mutex //

	seq uint64 //给发送得请求编号，每个请求唯一编号

	pending map[uint64]*Call //存储未处理的call

	closing bool //与shutdown表示client处于不可用的状态

	shutdown bool //shutdown置为true一般都是有错误发生
}

var _ io.Closer = (*Client)(nil)

var ErrShutdown = errors.New("connetction is shut down")

func (c * Client) Close() error{
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		return ErrShutdown
	}

	c.closing = true
	return c.cc.Close()
}

func (c * Client) IsAvailable() bool{
	c.mu.Lock()
	defer c.mu.Unlock()

	return !c.closing && !c.shutdown
}
//将call添加到pending中
func (c * Client) registerCall(call * Call ) (uint64 ,error){
	//加锁
	c.mu.Lock()
	//函数执行完后放锁
	defer c.mu.Unlock()
	if c.closing || c.shutdown{
		return 0,ErrShutdown
	}

	call.Seq = c.seq
	c.pending[call.Seq] = call
	c.seq ++

	return call.Seq,nil
}
//根据req从pending中删除指定的call，并返回指定的call
func (c * Client) removeCall(seq uint64) *Call{
	c.mu.Lock()
	defer c.mu.Unlock()
	//从待处理map中取出要删除的call
	call := c.pending[seq]
	//调用delete函数删除
	delete(c.pending,seq)

	return call
}
// 服务端或客户端发生错误时调用，将shutdown设置为true，表示错误。并将错误信息返回给所有未处理状态的call
func (c * Client) terminateCalls(err error){
	c.sending.Lock()
	defer c.sending.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()

	// 标识shutdown
	c.shutdown = true

	for _,call := range c.pending{
		call.Error = err
		call.done()
	}
}
//1.call不存在 2.call存在，但服务端出错 3.call存在，并在服务端没有出错
func (c * Client) receive(){
	var err error
	for err == nil{
		var h codec.Header
		if err = c.cc.ReadHeader(&h); err != nil{
			break
		}

		call := c.removeCall(h.Seq)
		switch{
		case call == nil:
			err = c.cc.ReadBody(nil)
		case h.Error != "":
			call.Error = fmt.Errorf(h.Error)
			err = c.cc.ReadBody(nil)
		default:
			err = c.cc.ReadBody(call.Reply)
			if err != nil{
				call.Error = errors.New("reading body " + err.Error())
			}
			call.done()
		}
	}
	c.terminateCalls(err)
}
//首先要和服务端定义好协议交换，也就是把option信息发送给服务端。协商好消息的解编码方式后，再开一个协程调用reveive()接收响应
func NewClient(conn net.Conn, opt * Option) (*Client ,error){

	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil{
		err := fmt.Errorf("invalid codec type %s",opt.CodecType)
		log.Println("rpc client : codec error:",err)
		return nil,err
	}

	//发送option给服务端
	if err := json.NewEncoder(conn).Encode(opt); err != nil{
		log.Println("rpc client:options error :",err)
		_ = conn.Close()
		return nil,err
	}

	return newClientCodec(f(conn),opt),nil
}

func newClientCodec(cc codec.Codec,opt * Option) *Client{
	client := &Client{
		seq: 1,
		cc : cc,
		opt: opt,
		pending: make(map[uint64]*Call),
	}
	go client.receive()
	return client
}
//转换option
func parseOptions(opts ...*Option) (*Option,error){
	if len(opts) == 0 || opts[0] == nil{
		//默认为gob
		return DefaultOption,nil
	}
	//给了大于1的opt时则报错
	if len(opts) != 1{
		return nil,errors.New("number of options is more than 1")
	}
	//opt为第一个opt
	opt := opts[0]
	opt.MagicNumber = DefaultOption.MagicNumber
	if opt.CodecType == ""{
		opt.CodecType = DefaultOption.CodecType
	}
	return opt,nil
}

func Dial(network,address string,opts ...*Option)(client *Client,err error)  {
	opt,err := parseOptions(opts...)
	if err != nil{
		return nil,err
	}

	conn,err := net.Dial(network,address)
	if err != nil{
		return nil,err
	}

	defer func() {
		if client == nil{
			_ = conn.Close()
		}
	}()

	return NewClient(conn,opt)
}
func (c * Client) send(call *Call){
	//先对sending加锁
	c.sending.Lock()
	defer c.sending.Unlock()
	//
	seq,err := c.registerCall(call)

	if err != nil{
		call.Error = err
		call.done()
		return
	}

	c.header.ServiceMethod = call.ServiceMethod
	c.header.Seq = seq
	c.header.Error = ""

	if err := c.cc.Write(&c.header,call.Args);err != nil{
		call := c.removeCall(seq)

		if call != nil{
			call.Error = err
			call.done()
		}
	}
}

func (c * Client) Go(serviceMethod string,args ,reply interface{},done chan *Call)  *Call{

	if done == nil{
		done = make(chan *Call,10)
	}else if cap(done) == 0{
		log.Panic("rpc client ：done channel is unbuffered")
	}
	call := &Call{
		ServiceMethod: serviceMethod,
		Args: args,
		Reply: reply,
		Done: done,
	}
	c.send(call)
	return call
}

func (c * Client) Call(serviceMethod string,args,reply interface{})error  {
	call := <-c.Go(serviceMethod,args,reply,make(chan *Call,1)).Done
	return call.Error

}