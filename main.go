package main

import (
 "bufio"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "log"
 "net"
 "net/http"
 "net/url"
 "os"
 "os/signal"
 "strconv"
 "strings"
 "syscall"
 "time"
)

type event struct {
 Type string `json:"Type"`
 Action string `json:"Action"`
 Actor struct { ID string `json:"ID"`; Attributes map[string]string `json:"Attributes"` } `json:"Actor"`
 Time int64 `json:"time"`
 TimeNano int64 `json:"timeNano"`
}
var allowedTypes=map[string]bool{"container":true}
var allowedActions=map[string]bool{"create":true,"start":true,"stop":true,"die":true,"destroy":true,"kill":true,"oom":true,"pause":true,"unpause":true,"restart":true,"health_status: healthy":true,"health_status: unhealthy":true,"rename":true,"update":true}
func safe(s string,max int)string{
 if len(s)>max{s=s[:max]}
 var b strings.Builder
 for _,r:=range s {if (r>='a'&&r<='z')||(r>='A'&&r<='Z')||(r>='0'&&r<='9')||strings.ContainsRune("._:/@- ",r){b.WriteRune(r)}else{b.WriteByte('_')}}
 return b.String()
}
// classifyExec exports only fixed classifications. Raw arguments can contain
// secrets, and must never be forwarded to Vector, Wazuh or application logs.
func classifyExec(action string) (string,string) {
 if !strings.HasPrefix(action,"exec_start:"){return "",""}
 fields:=strings.Fields(strings.TrimPrefix(action,"exec_start:"))
 if len(fields)==0{return "other","other"}
 name:=fields[0]
 if i:=strings.LastIndexByte(name,'/');i>=0{name=name[i+1:]}
 switch name {
 case "sh","bash","ash","dash","zsh","fish":
  for _,flag:=range fields[1:] {
   if !strings.HasPrefix(flag,"-"){break}
   if flag=="--command" || (len(flag)>1 && !strings.HasPrefix(flag,"--") && strings.ContainsRune(flag[1:],'c')) {
    return "shell_command",name
   }
  }
  return "shell_direct",name
 case "nsenter","unshare","setpriv","capsh","mount","modprobe","insmod","chroot":
  return "privileged_tool",name
 case "curl":
  if strings.Contains(action,"--unix-socket") &&
    (strings.Contains(action,"/var/run/docker.sock") || strings.Contains(action,"/run/docker.sock")) {
   return "docker_socket_access",name
  }
  return "network_tool",name
 case "wget","nc","ncat","socat":
  return "network_tool",name
 case "docker":
  return "docker_cli",name
 default:
  return "other","other"
 }
}

func format(e event,host string)string{
 a:=e.Actor.Attributes
 ts:=time.Unix(e.Time,0).UTC()
 if e.TimeNano>0 {ts=time.Unix(0,e.TimeNano).UTC()}
 message:=map[string]any{"source":"docker-events","host":host,"type":e.Type,"action":e.Action,"id":safe(e.Actor.ID,64),"name":safe(a["name"],128),"image":safe(a["image"],256),"project":safe(a["com.docker.compose.project"],128),"exit_code":safe(a["exitCode"],8),"time":ts.Format(time.RFC3339Nano)}
 if class,tool:=classifyExec(e.Action);class!="" {
  message["action"]="exec_start"
  message["exec_class"]=class
  message["exec_tool"]=tool
 }
 raw,_:=json.Marshal(message)
 return fmt.Sprintf("<134>1 %s %s docker-events - - - %s\n",ts.Format(time.RFC3339Nano),safe(host,64),raw)
}
func connect(ctx context.Context,addr string)(net.Conn,error){
 d:=net.Dialer{Timeout:5*time.Second,KeepAlive:30*time.Second}
 return d.DialContext(ctx,"tcp",addr)
}
func send(ctx context.Context,addr,payload string)error{
 c,err:=connect(ctx,addr);if err!=nil{return err};defer c.Close()
 if err=c.SetWriteDeadline(time.Now().Add(5*time.Second));err!=nil{return err}
 n,err:=io.WriteString(c,payload);if err==nil&&n!=len(payload){return io.ErrShortWrite};return err
}
// The cursor is volatile and is never a forensic checkpoint. Docker's
// 'since' parameter is inclusive at second resolution.
type cursor struct {
 nano int64
 key string
}
func eventNano(e event) int64 {
 if e.TimeNano>0{return e.TimeNano}
 return e.Time*int64(time.Second)
}
func eventKey(e event)string{return e.Type+"\x00"+e.Action+"\x00"+e.Actor.ID}
func (c *cursor) already(e event)bool{
 n:=eventNano(e)
 return n<c.nano || n==c.nano && eventKey(e)==c.key
}
func (c *cursor) mark(e event){
 c.nano=eventNano(e)
 c.key=eventKey(e)
}
func poll(ctx context.Context,client *http.Client,endpoint,out,host string,mark *cursor)error{
 u,err:=url.Parse(endpoint);if err!=nil{return err}
 q:=u.Query()
 q.Set("since",strconv.FormatInt(mark.nano/int64(time.Second),10))
 q.Set("filters","{\"type\":[\"container\"]}")
 u.RawQuery=q.Encode()
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,u.String(),nil);if err!=nil{return err}
 resp,err:=client.Do(req);if err!=nil{return err};defer resp.Body.Close()
 if resp.StatusCode!=http.StatusOK{return fmt.Errorf("Docker events HTTP %d",resp.StatusCode)}
 scan:=bufio.NewScanner(resp.Body)
 scan.Buffer(make([]byte,16*1024),128*1024)
 for scan.Scan(){
  var ev event
  if err:=json.Unmarshal(scan.Bytes(),&ev);err!=nil {
   log.Print("invalid Docker event JSON ignored")
   continue
  }
  if !allowedTypes[ev.Type]||(!allowedActions[ev.Action] && !strings.HasPrefix(ev.Action,"exec_start:"))||ev.Time<=0||mark.already(ev){continue}
  line:=format(ev,host)
  if err=send(ctx,out,line);err!=nil{return fmt.Errorf("Vector delivery failed: %w",err)}
  mark.mark(ev)
 }
 if err:=scan.Err();err!=nil{return fmt.Errorf("Docker event stream failed: %w",err)}
 return io.EOF
}
func run(ctx context.Context)error{
 endpoint:=os.Getenv("DOCKER_EVENTS_URL");if endpoint==""{endpoint="http://docker-socket:2375/events"}
 u,err:=url.Parse(endpoint);if err!=nil||u.Scheme!="http"||u.Host!="docker-socket:2375"||u.Path!="/events"{return errors.New("DOCKER_EVENTS_URL must use the dedicated docker-socket:2375 events endpoint")}
 out:=os.Getenv("VECTOR_ADDR");if out==""{return errors.New("VECTOR_ADDR is required")}
 if _,_,err=net.SplitHostPort(out);err!=nil{return fmt.Errorf("invalid VECTOR_ADDR: %w",err)}
 host:=os.Getenv("EVENT_HOST");if host==""{host="tower"}
 transport:=&http.Transport{Proxy:nil,DialContext:(&net.Dialer{Timeout:5*time.Second,KeepAlive:30*time.Second}).DialContext,MaxIdleConns:1,IdleConnTimeout:30*time.Second,ResponseHeaderTimeout:10*time.Second,DisableCompression:true}
 defer transport.CloseIdleConnections()
 client:=&http.Client{Transport:transport}
 mark:=cursor{nano:time.Now().Add(-2*time.Second).UnixNano()}
 backoff:=time.Second
 for ctx.Err()==nil {
  err=poll(ctx,client,endpoint,out,host,&mark)
  if ctx.Err()!=nil {break}
  log.Printf("event stream disconnected (%v); reconnecting",err)
  select{case <-time.After(backoff):case <-ctx.Done():return nil}
  if backoff<30*time.Second{backoff*=2}
 }
 return nil
}
func main(){
 log.SetFlags(log.LstdFlags|log.LUTC)
 ctx,stop:=signal.NotifyContext(context.Background(),syscall.SIGTERM,syscall.SIGINT);defer stop()
 if err:=run(ctx);err!=nil{log.Fatal(err)}
}
