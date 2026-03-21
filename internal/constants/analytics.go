package constants

// WEB_ANALYTICS_SCRIPT is the inline JavaScript tracker injected into deployed
// Next.js apps. It auto-tracks pageviews, page leaves (with scroll depth and
// duration), handles SPA navigation (pushState/replaceState/popstate), parses
// UTM params, and exposes window.wmTrack(name, props) for custom events.
// Privacy-friendly: no cookies, uses localStorage for visitor ID and
// sessionStorage for session ID. Sends data via navigator.sendBeacon().
// The placeholder __PROJECT_ID__ is replaced at injection time.
// WEB_ANALYTICS_SCRIPT_BODY contains only the JS code (no script tags).
// The injection function wraps it appropriately for JSX or HTML targets.
const WEB_ANALYTICS_SCRIPT_BODY = `(function(){
  var API="https://api.wemob.io/v1/analytics/collect";
  var PID="__PROJECT_ID__";
  if(!PID||PID==="__"+"PROJECT_ID__")return;
  function gid(k,s){var st=s?sessionStorage:localStorage;var v=st.getItem(k);if(!v){try{v=crypto.randomUUID()}catch(e){v="xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g,function(c){var r=Math.random()*16|0;return(c==="x"?r:(r&0x3|0x8)).toString(16)})}st.setItem(k,v)}return v}
  var vid=gid("wm_vid",false);
  var sid=gid("wm_sid",true);
  var start=Date.now();
  var maxScroll=0;
  var utm={};
  try{var p=new URLSearchParams(location.search);["utm_source","utm_medium","utm_campaign","utm_term","utm_content"].forEach(function(k){var v=p.get(k);if(v)utm[k]=v})}catch(e){}
  function scroll(){var h=document.documentElement;var s=h.scrollHeight-h.clientHeight;if(s>0)maxScroll=Math.max(maxScroll,Math.round((window.scrollY/s)*100))}
  window.addEventListener("scroll",scroll,{passive:true});
  function send(type,extra){
    var d={project_id:PID,visitor_id:vid,session_id:sid,event_type:type,url:location.href,path:location.pathname,referrer:document.referrer,title:document.title,screen_size:screen.width+"x"+screen.height,language:navigator.language};
    Object.keys(utm).forEach(function(k){d[k]=utm[k]});
    if(extra)Object.keys(extra).forEach(function(k){d[k]=extra[k]});
    var b=JSON.stringify(d);try{navigator.sendBeacon(API,new Blob([b],{type:"application/json"}))}catch(e){try{fetch(API,{method:"POST",headers:{"Content-Type":"application/json"},body:b,keepalive:true})}catch(e2){}}
  }
  send("pageview");
  document.addEventListener("visibilitychange",function(){
    if(document.visibilityState==="hidden"){send("pageleave",{duration:Math.round((Date.now()-start)/1000),scroll_depth:maxScroll})}
  });
  var origPush=history.pushState;var origReplace=history.replaceState;
  function onNav(){start=Date.now();maxScroll=0;setTimeout(function(){send("pageview")},0)}
  history.pushState=function(){origPush.apply(this,arguments);onNav()};
  history.replaceState=function(){origReplace.apply(this,arguments);onNav()};
  window.addEventListener("popstate",onNav);
  window.wmTrack=function(name,props){send("event",{event_name:name,properties:props||{}})};
})();`
