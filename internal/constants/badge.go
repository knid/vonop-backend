package constants

// WEMOB_BADGE_SCRIPT is inline JS that renders a "Created by Wemob" badge
// in the bottom-right corner of deployed apps. Injected at deploy time
// for users whose plan does not include CanRemoveBrandLogo.
// Pure JS + inline styles — no framework dependencies.
const WEMOB_BADGE_SCRIPT = `(function(){
  if(document.querySelector('[data-wemob-badge]')) return;
  function inject(){
    if(!document.body) return;
    var a=document.createElement('a');
    a.href='https://wemob.io?ref=badge';
    a.target='_blank';
    a.rel='noopener noreferrer';
    a.setAttribute('data-wemob-badge','1');
    a.style.cssText='position:fixed;bottom:16px;right:16px;z-index:9999;display:flex;align-items:center;gap:6px;padding:6px 12px;background:rgba(0,0,0,0.85);color:#fff;font-family:-apple-system,BlinkMacSystemFont,Segoe UI,Roboto,sans-serif;font-size:12px;font-weight:500;border-radius:20px;text-decoration:none;backdrop-filter:blur(8px);-webkit-backdrop-filter:blur(8px);box-shadow:0 2px 8px rgba(0,0,0,0.2);transition:opacity 0.2s;opacity:0.9;';
    a.onmouseenter=function(){a.style.opacity='1'};
    a.onmouseleave=function(){a.style.opacity='0.9'};
    var ns='http://www.w3.org/2000/svg';
    var svg=document.createElementNS(ns,'svg');
    svg.setAttribute('width','14');
    svg.setAttribute('height','14');
    svg.setAttribute('viewBox','0 0 24 24');
    svg.setAttribute('fill','none');
    var circle=document.createElementNS(ns,'circle');
    circle.setAttribute('cx','12');circle.setAttribute('cy','12');circle.setAttribute('r','10');
    circle.setAttribute('stroke','white');circle.setAttribute('stroke-width','2');
    var path=document.createElementNS(ns,'path');
    path.setAttribute('d','M8 12l3 3 5-5');
    path.setAttribute('stroke','white');path.setAttribute('stroke-width','2');
    path.setAttribute('stroke-linecap','round');path.setAttribute('stroke-linejoin','round');
    svg.appendChild(circle);svg.appendChild(path);
    a.appendChild(svg);
    var t=document.createTextNode(' Created by ');
    a.appendChild(t);
    var b=document.createElement('span');
    b.style.fontWeight='700';
    b.textContent='Wemob';
    a.appendChild(b);
    document.body.appendChild(a);
  }
  if(document.readyState==='loading'){
    document.addEventListener('DOMContentLoaded',function(){requestAnimationFrame(inject);});
  }else{
    requestAnimationFrame(inject);
  }
})();`
