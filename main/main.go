package main

import (
	"fmt"
	"grlrpc"
	"log"
	"net"
	"sync"
	"time"
)

func startServer(addr chan string)  {
	l,err := net.Listen("tcp",":0")
	if err != nil{
		log.Fatal("network error :",err)
	}
	log.Println("start rpc server on",l.Addr())
	addr <-l.Addr().String()
	grlrpc.Accept(l)
}

func main() {
	log.SetFlags(0)
	addr := make(chan string)
	go startServer(addr)
	client,_ := grlrpc.Dial("tcp",<-addr)
	//关闭客户端
	defer func() { _ = client.Close()}()
	time.Sleep(time.Second)

	var wg sync.WaitGroup

	for i := 0;i<5;i++{
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := fmt.Sprintf("grlrpc req %d",i)
			var reply string
			if err := client.Call("Foo.sum",args,&reply);err != nil{
				log.Fatal("call Foo.sum error:",err)
			}
			log.Println("reply:",reply)
		}(i)
	}
	wg.Wait()
}
