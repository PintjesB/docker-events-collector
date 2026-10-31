package main

import (
 "bufio"
 "context"
 "encoding/json"
 "fmt"
 "io"
 "os"
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

func TestDockerEndpointValidation(t *testing.T) {
 original, wasSet := os.LookupEnv("DOCKER_EVENTS_URL")
 defer func(){ if wasSet { _=os.Setenv("DOCKER_EVENTS_URL",original) } else { _=os.Unsetenv("DOCKER_EVENTS_URL") } }()
 cases:=[]struct{url string;accepted bool}{
  {"http://docker-socket:2375/events",true},
  {"http://docker-socket:2375/v1.41/events",false},
  {"http://evil-host:2375/events",false},
  {"https://docker-socket:2375/events",false},
  {"http://docker-socket:2375/containers/json",false},
 }
 // Verify the URL policy without opening network connections: invalid
 // inputs must be rejected by run before any dial occurs.
 for _,tc:=range cases {
  if tc.accepted {continue}
  t.Run(tc.url,func(t *testing.T){
   t.Setenv("DOCKER_EVENTS_URL",tc.url)
   t.Setenv("VECTOR_ADDR","127.0.0.1:1")
   ctx,cancel:=context.WithTimeout(context.Background(),50*time.Millisecond); defer cancel()
   err:=run(ctx)
   if err==nil||!strings.Contains(err.Error(),"DOCKER_EVENTS_URL"){t.Fatalf("unsafe Docker URL accepted: %q, err=%v",tc.url,err)}
  })
 }
}

func TestExecTelemetryNeverForwardsArguments(t *testing.T) {
 cases:=[]struct{action,kind,tool string}{
  {"exec_start: /bin/sh","shell_direct","sh"},
  {"exec_start: /bin/bash -c echo SUPER_SECRET_TOKEN","shell_command","bash"},
  {"exec_start: /usr/bin/nsenter --target 1 --mount","privileged_tool","nsenter"},
  {"exec_start: curl --unix-socket /var/run/docker.sock http://localhost/containers/json","docker_socket_access","curl"},
  {"exec_start: /usr/bin/curl https://example.test","network_tool","curl"},
  {"exec_start: docker ps","docker_cli","docker"},
  {"exec_start: /bin/true --password=SUPER_SECRET_TOKEN","other","other"},
  {"exec_start: /weird/SUPER_SECRET_TOKEN --user=root","other","other"},
 }
 for _,tc:=range cases{
  kind,tool:=classifyExec(tc.action)
  if kind!=tc.kind||tool!=tc.tool {t.Fatalf("%q: got %s/%s",tc.action,kind,tool)}
  ev:=event{Type:"container",Action:tc.action,Time:time.Now().Unix()}
  ev.Actor.ID="012345"
  ev.Actor.Attributes=map[string]string{"name":"test-container"}
  logline:=format(ev,"tower")
  if strings.Contains(logline,"SUPER_SECRET_TOKEN")||strings.Contains(logline,"--unix-socket")||
     strings.Contains(logline,"http://localhost/containers/json")||strings.Contains(logline,"--target")||
     strings.Contains(logline,"example.test")||strings.Contains(logline,"-c echo") {
   t.Fatalf("sensitive exec arguments leaked: %q",logline)
  }
  if !strings.Contains(logline,"\"action\":\"exec_start\"")||
     !strings.Contains(logline,"\"exec_class\":\""+tc.kind+"\"")||
     !strings.Contains(logline,"\"exec_tool\":\""+tc.tool+"\"") {
   t.Fatalf("missing sanitized telemetry: %q",logline)
  }
 }
 if kind,tool:=classifyExec("exec_create: /bin/sh -c SECRET");kind!=""||tool!="" {
  t.Fatal("exec_create should not be forwarded")
 }
}
