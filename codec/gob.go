package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

type GobCodec struct {
	conn io.ReadWriteCloser
	buf *bufio.Writer
	dec *gob.Decoder
	enc *gob.Encoder
}
var _ Codec = (*GobCodec)(nil)

func NewGobCodec(conn io.ReadWriteCloser) Codec {
	buf := bufio.NewWriter(conn)
	return &GobCodec{
		conn: conn,
		buf:  buf,
		dec:  gob.NewDecoder(conn),
		enc:  gob.NewEncoder(buf),
	}
}

func (c * GobCodec) Close() error {
	return c.conn.Close()
}

func (c * GobCodec) ReadHeader(header *Header) error {
	return c.dec.Decode(header)
}

func (c * GobCodec) ReadBody(i interface{}) error {
	return c.dec.Decode(i)
}

func (c * GobCodec) Write(header *Header, i interface{}) (err error) {
	defer func() {
		_ = c.buf.Flush()
		if err != nil{
			_ = c.Close()
		}
	}()

	if err := c.enc.Encode(header); err != nil{
		log.Println("rpc codec : gob error encoding header :",err)
		return err
	}

	if err := c.enc.Encode(i);err != nil{
		log.Println("rpc codec : gob error encoding body : ",err)
		return err
	}

	return nil


}
