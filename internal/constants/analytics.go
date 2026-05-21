package constants

// WEMOB_ANALYTICS_SCRIPT is a self-contained JS tracker injected into deployed
// apps at build time. It captures pageviews, pageleaves (with duration), referrer,
// UTM params, screen size, and language, then POSTs to the analytics collect
// endpoint. The string __WEMOB_PROJECT_ID__ is replaced with the actual project
// UUID at deploy time.
const WEMOB_ANALYTICS_SCRIPT = `(function(){
  var PROJECT_ID='__WEMOB_PROJECT_ID__';
  var ENDPOINT='https://api.wemob.io/v1/analytics/collect';
  function getStorage(k){try{return localStorage.getItem(k);}catch(e){return null;}}
  function setStorage(k,v){try{localStorage.setItem(k,v);}catch(e){}}
  function genId(){return Math.random().toString(36).slice(2,11)+Math.random().toString(36).slice(2,11);}

  var vid=getStorage('wemob_vid')||genId();
  setStorage('wemob_vid',vid);
  var sid=getStorage('wemob_sid');
  var ss=parseInt(getStorage('wemob_ss')||'0',10);
  var now=Date.now();
  if(!sid||now-ss>30*60*1000){sid=genId();setStorage('wemob_sid',sid);setStorage('wemob_ss',String(now));}

  function getUTM(n){var m=location.search.match(new RegExp('[?&]'+n+'=([^&]+)'));return m?decodeURIComponent(m[1]):'';}

  function send(type,extra){
    var payload={
      project_id:PROJECT_ID,visitor_id:vid,session_id:sid,event_type:type,
      url:location.href,path:location.pathname+location.search,
      referrer:document.referrer||'',title:document.title||'',
      screen_size:(screen.width||0)+'x'+(screen.height||0),language:(navigator.language||''),
      utm_source:getUTM('utm_source'),utm_medium:getUTM('utm_medium'),
      utm_campaign:getUTM('utm_campaign'),utm_term:getUTM('utm_term'),utm_content:getUTM('utm_content')
    };
    if(extra)for(var k in extra)payload[k]=extra[k];
    fetch(ENDPOINT,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload),keepalive:true}).catch(function(){});
  }

  var startTime=Date.now();
  send('pageview');

  function sendLeave(){send('pageleave',{duration:Math.round((Date.now()-startTime)/1000)});}
  window.addEventListener('pagehide',sendLeave);
  window.addEventListener('beforeunload',sendLeave);

  var origPush=history.pushState;
  var origReplace=history.replaceState;
  function onNav(){
    sendLeave();
    startTime=Date.now();
    send('pageview');
    setStorage('wemob_ss',String(Date.now()));
  }
  history.pushState=function(){origPush.apply(this,arguments);onNav();};
  history.replaceState=function(){origReplace.apply(this,arguments);onNav();};
  window.addEventListener('popstate',onNav);
})();`
