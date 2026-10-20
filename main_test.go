package main

import (
 "encoding/json"
 "strings"
 "testing"
 "time"
)
func TestAllowlist(t *testing.T){
 if allowedTypes["service"]||allowedActions["exec_start"]||!allowedActions["die"]{t.Fatal("unsafe event allowlist")}
}
func TestSanitize(t *testing.T){
 x:=safe("abc\nxyz\r",100)
 if strings.ContainsAny(x,"\r\n"){t.Fatal("control characters")}
}
func TestFormatSanitized(t *testing.T){
 var e event
 err:=json.Unmarshal([]byte(`{"Type":"container","Action":"die","Actor":{"ID":"abc","Attributes":{"name":"foo\\nbar","image":"example:1","exitCode":"137","secret":"MUST_NOT_LEAK"}},"time":1700000000}`),&e)
 if err!=nil{t.Fatal(err)}
 v:=format(e,"tower")
 if strings.Contains(v,"MUST_NOT_LEAK")||!strings.Contains(v,"docker-events")||!strings.HasSuffix(v,"\n"){t.Fatal(v)}
 if time.Unix(e.Time,0).IsZero(){t.Fatal("bad timestamp")}
}
