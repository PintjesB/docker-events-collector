package main

import (
 "bufio"
 "context"
 "encoding/json"
 "fmt"
 "io"
 "net"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"
)
func sample(action string, n int64) string {
 e:=event{Type:"container", Action:action, Time:n/int64(time.Second),TimeNano:n}
 e.Actor.ID="a012"
 e.Actor.Attributes=map[string]string{"name":"web","image":"nginx:1","com.docker.compose.project":"my-project","exitCode":"137","secret":"LEAK_ME"}
 b,_:=json.Marshal(e)
 return string(b)
}
func TestSanitization(t *testing.T){
 if safe("danger\nline",50)!="danger_line"{t.Fatal("control character not sanitized")}
 if allowedActions["exec_start"]||!allowedActions["oom"]||allowedTypes["network"]{t.Fatal("allowlist too broad")}
 var ev event
 if err:=json.Unmarshal([]byte(sample("die",time.Now().UnixNano())),&ev);err!=nil{t.Fatal(err)}
 line:=format(ev,"tower")
 if strings.Contains(line,"LEAK_ME")||!strings.Contains(line,"\"exit_code\":\"137\"")||!strings.HasPrefix(line,"<134>1 ")||!strings.HasSuffix(line,"\n"){t.Fatal(line)}
}
func TestCursor(t *testing.T){
 n:=time.Now().UnixNano()
 var a,b event
 _=json.Unmarshal([]byte(sample("die",n)),&a)
 _=json.Unmarshal([]byte(sample("oom",n)),&b)
 c:=cursor{}
 if c.already(a){t.Fatal("new event skipped")}
 c.mark(a)
 if !c.already(a)||c.already(b){t.Fatal("timestamp and identity deduplication mismatch")}
}
func TestEndToEndVectorForwardAndReplay(t *testing.T){
 listener,err:=net.Listen("tcp","127.0.0.1:0")
 if err!=nil{t.Fatal(err)}
 defer listener.Close()
 received:=make(chan string,2)
 go func(){
  c,e:=listener.Accept()
  if e!=nil{return}
  defer c.Close()
  _=c.SetReadDeadline(time.Now().Add(3*time.Second))
  s,e:=bufio.NewReader(c).ReadString('\n')
  if e==nil{received<-s}
 }()
 n:=time.Now().UnixNano()
 api:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Query().Get("since")==""||!strings.Contains(r.URL.Query().Get("filters"),"container"){t.Error("Docker filters missing")}
  _,_=fmt.Fprintln(w,sample("die",n))
 }))
 defer api.Close()
 mark:=cursor{nano:n-int64(time.Second)}
 if err:=poll(context.Background(),api.Client(),api.URL,listener.Addr().String(),"tower",&mark);err!=io.EOF{t.Fatal(err)}
 if err:=poll(context.Background(),api.Client(),api.URL,listener.Addr().String(),"tower",&mark);err!=io.EOF{t.Fatal(err)}
 select{
 case s:=<-received:
  if !strings.Contains(s,"\"action\":\"die\""){t.Fatal(s)}
 case <-time.After(3*time.Second):t.Fatal("Vector never received the syslog event")
 }
 if mark.nano!=n{t.Fatal("cursor not updated")}
}
func TestFailedForwardDoesNotAdvanceCursor(t *testing.T){
 failed,err:=net.Listen("tcp","127.0.0.1:0")
 if err!=nil{t.Fatal(err)}
 addr:=failed.Addr().String()
 _=failed.Close()
 n:=time.Now().UnixNano()
 api:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){_,_=fmt.Fprintln(w,sample("oom",n))}))
 defer api.Close()
 mark:=cursor{nano:n-int64(time.Second)}
 err=poll(context.Background(),api.Client(),api.URL,addr,"tower",&mark)
 if err==nil||mark.nano!=n-int64(time.Second){t.Fatal("acknowledged undelivered event")}
}
func TestOversizedEventIsRejected(t *testing.T){
 api:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  _,_=fmt.Fprintln(w,strings.Repeat("X",200000))
 }))
 defer api.Close()
 mark:=cursor{nano:time.Now().UnixNano()}
 err:=poll(context.Background(),api.Client(),api.URL,"127.0.0.1:1","tower",&mark)
 if err==nil||!strings.Contains(err.Error(),"Docker event stream failed"){t.Fatalf("expected bounded scanner error: %v",err)}
}
