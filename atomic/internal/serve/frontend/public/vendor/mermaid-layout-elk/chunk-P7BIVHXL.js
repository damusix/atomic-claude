import{a as Ct}from"./chunk-W2DK7OQN.js";import{a as wt}from"./chunk-X3PD67MY.js";import"./chunk-R7U6BSD3.js";import{a as bt}from"./chunk-NXUTASGL.js";import"./chunk-ECTZL2JS.js";import"./chunk-6UPRXX3O.js";import"./chunk-SHHEBKDM.js";import"./chunk-CBM6V7SV.js";import"./chunk-CESICB5C.js";import"./chunk-WUOCHPBX.js";import"./chunk-NA4UVSVP.js";import"./chunk-UNB6RQ3C.js";import"./chunk-TNB4RAFO.js";import"./chunk-TOEQHOJY.js";import"./chunk-5MPVYCPH.js";import"./chunk-NCMKR4QE.js";import"./chunk-QTWQIMGH.js";import"./chunk-IS6DLGVL.js";import"./chunk-6FNSYWGJ.js";import{o as j}from"./chunk-RANDTXFT.js";import"./chunk-PAL66KRM.js";import{O as mt,T as pt,U as yt,V as ut,W as ht,X as gt,Y as xt,Z as $t,i as Z,k as ft,u as X}from"./chunk-BAGAVWDO.js";import{b as G}from"./chunk-VYE4DNDP.js";import{a as r}from"./chunk-NKM6JPMV.js";import{d as z}from"./chunk-RPKWB5KU.js";var Dt=r(()=>({domains:new Map,transitions:[]}),"createDefaultData"),Y=Dt(),Mt=r(()=>Y.domains,"getDomains"),zt=r(()=>Y.transitions,"getTransitions"),Pt=r(t=>{if(t)for(let e of t){let n=e.domain,o=(e.items??[]).map(c=>({label:c.label}));Y.domains.set(n,{name:n,items:o})}},"setDomains"),Lt=r(t=>{t&&(Y.transitions=t.filter(e=>e.from===e.to?(G.warn(`Cynefin: self-loop transition on domain "${e.from}" is not meaningful and will be skipped.`),!1):!0).map(e=>({from:e.from,to:e.to,label:e.label||void 0})))},"setTransitions"),_t=r(()=>j({...ft.cynefin,...X().cynefin}),"getConfig"),Nt=r(()=>{pt(),Y=Dt()},"clear"),q={getDomains:Mt,getTransitions:zt,setDomains:Pt,setTransitions:Lt,getConfig:_t,clear:Nt,setAccTitle:yt,getAccTitle:ut,setDiagramTitle:xt,getDiagramTitle:$t,getAccDescription:gt,setAccDescription:ht},It=r(t=>{Ct(t,q),q.setDomains(t.domains),q.setTransitions(t.transitions)},"populate"),Rt={parse:r(async t=>{let e=await wt("cynefin",t);G.debug(e),It(e)},"parse")};function E(t){let e=t+1831565813|0;return e=Math.imul(e^e>>>15,e|1),e^=e+Math.imul(e^e>>>7,e|61),((e^e>>>14)>>>0)/4294967296}z(E,"seededRandom");r(E,"seededRandom");function K(t){let e=0;for(let n=0;n<t.length;n++){let o=t.charCodeAt(n);e=(e<<5)-e+o,e|=0}return e}z(K,"hashString");r(K,"hashString");function tt(t,e){return typeof t=="number"&&Number.isFinite(t)&&t!==0?t:K(e)}z(tt,"resolveSeed");r(tt,"resolveSeed");function et(t,e,n,o){let c=t/2,m=o??t*.015,v=7,R=e/v,d=[];for(let a=0;a<=v;a++){let p=E(n+a*17)*m*2-m;d.push({x:c+p,y:a*R})}let D=`M${d[0].x},${d[0].y}`;for(let a=0;a<d.length-1;a++){let p=d[a],s=d[a+1],f=(p.y+s.y)/2,b=a%2===0?1:-1,h=m*1.5*b*E(n+a*31+7),W=p.x+h,F=f,H=s.x-h;D+=` C${W},${F} ${H},${f} ${s.x},${s.y}`}return D}z(et,"generateFoldPath");r(et,"generateFoldPath");function nt(t,e,n,o){let c=e/2,m=o??e*.015,v=7,R=t/v,d=[];for(let a=0;a<=v;a++){let p=E(n+a*23)*m*2-m;d.push({x:a*R,y:c+p})}let D=`M${d[0].x},${d[0].y}`;for(let a=0;a<d.length-1;a++){let p=d[a],s=d[a+1],f=(p.x+s.x)/2,b=a%2===0?1:-1,h=m*1.5*b*E(n+a*37+11),W=f,F=p.y+h,H=f,P=s.y-h;D+=` C${W},${F} ${H},${P} ${s.x},${s.y}`}return D}z(nt,"generateHorizontalBoundary");r(nt,"generateHorizontalBoundary");function ot(t,e){let n=t/2,o=e*.5,c=e,m=t*.03;return[`M${n},${o}`,`C${n+m},${o+(c-o)*.2}`,`${n-m*1.5},${o+(c-o)*.55}`,`${n+m*.5},${o+(c-o)*.75}`,`C${n-m},${o+(c-o)*.85}`,`${n+m*.3},${o+(c-o)*.95}`,`${n},${c}`].join(" ")}z(ot,"generateCliffPath");r(ot,"generateCliffPath");function at(t,e,n,o){return[`M${t-n},${e}`,`A${n},${o} 0 1,1 ${t+n},${e}`,`A${n},${o} 0 1,1 ${t-n},${e}`,"Z"].join(" ")}z(at,"generateConfusionPath");r(at,"generateConfusionPath");var vt={complex:{model:"Probe \u2192 Sense \u2192 Respond",practice:"Emergent Practices"},complicated:{model:"Sense \u2192 Analyse \u2192 Respond",practice:"Good Practices"},clear:{model:"Sense \u2192 Categorise \u2192 Respond",practice:"Best Practices"},chaotic:{model:"Act \u2192 Sense \u2192 Respond",practice:"Novel Practices"},confusion:{model:"",practice:"Disorder"}},Wt=r((t,e)=>{let n=t/2,o=e/2;return{complex:{cx:n/2,cy:o/2,x:0,y:0,w:n,h:o},complicated:{cx:n+n/2,cy:o/2,x:n,y:0,w:n,h:o},chaotic:{cx:n/2,cy:o+o/2,x:0,y:o,w:n,h:o},clear:{cx:n+n/2,cy:o+o/2,x:n,y:o,w:n,h:o},confusion:{cx:n,cy:o,x:n*.7,y:o*.7,w:n*.6,h:o*.6}}},"getDomainLayouts"),Ft=r(()=>{let t=Z(),e=X();return j(t,e.themeVariables).cynefin},"getCynefinDomainColors"),J=3,Ht=r((t,e,n,o)=>{let c=o.db,m=c.getDomains(),v=c.getTransitions(),R=c.getDiagramTitle(),d=c.getAccTitle(),D=c.getAccDescription(),a=c.getConfig(),p=Ft();G.debug("Rendering Cynefin diagram");let s=a.width,f=a.height,b=a.padding,h=a.showDomainDescriptions,W=a.boundaryAmplitude,F=s+b*2,H=f+b*2,P={complex:p.complexBg,complicated:p.complicatedBg,clear:p.clearBg,chaotic:p.chaoticBg,confusion:p.confusionBg},k=bt(e);mt(k,H,F,a.useMaxWidth??!0),k.attr("viewBox",`0 0 ${F} ${H}`),d&&k.append("title").text(d),D&&k.append("desc").text(D);let T=k.append("g").attr("transform",`translate(${b}, ${b})`),V=Wt(s,f),rt=tt(a.seed,e),kt=T.append("g").attr("class","cynefin-backgrounds"),U=["complex","complicated","chaotic","clear"];for(let l of U){let i=V[l];kt.append("rect").attr("class","cynefinDomain").attr("x",i.x).attr("y",i.y).attr("width",i.w).attr("height",i.h).attr("fill",P[l]).attr("fill-opacity",.4).attr("stroke","none")}let Q=T.append("g").attr("class","cynefin-boundaries");Q.append("path").attr("class","cynefinBoundary").attr("d",et(s,f,rt,W)).attr("fill","none"),Q.append("path").attr("class","cynefinBoundary").attr("d",nt(s,f,rt+100,W)).attr("fill","none"),Q.append("path").attr("class","cynefinCliff").attr("d",ot(s,f)).attr("fill","none");let Tt=s*.15,At=f*.15;T.append("path").attr("class","cynefinConfusion").attr("d",at(s/2,f/2,Tt,At)).attr("fill",P.confusion).attr("fill-opacity",.5);let it=T.append("g").attr("class","cynefin-labels");for(let l of U){let i=V[l];it.append("text").attr("class","cynefinDomainLabel").attr("x",i.cx).attr("y",h?i.cy-30:i.cy).attr("text-anchor","middle").attr("dominant-baseline","middle").text(l.charAt(0).toUpperCase()+l.slice(1))}if(it.append("text").attr("class","cynefinDomainLabel").attr("x",s/2).attr("y",h?f/2-10:f/2).attr("text-anchor","middle").attr("dominant-baseline","middle").text("Confusion"),h){let l=T.append("g").attr("class","cynefin-subtitles");for(let i of U){let u=V[i],y=vt[i];l.append("text").attr("class","cynefinSubtitle").attr("x",u.cx).attr("y",u.cy-10).attr("text-anchor","middle").attr("dominant-baseline","middle").text(y.model),l.append("text").attr("class","cynefinSubtitle").attr("x",u.cx).attr("y",u.cy+5).attr("text-anchor","middle").attr("dominant-baseline","middle").text(y.practice)}l.append("text").attr("class","cynefinSubtitle").attr("x",s/2).attr("y",f/2+8).attr("text-anchor","middle").attr("dominant-baseline","middle").text(vt.confusion.practice)}let st=T.append("g").attr("class","cynefin-items"),A=26,ct=10,Bt=["complex","complicated","chaotic","clear","confusion"];for(let l of Bt){let i=m.get(l);if(!i||i.items.length===0)continue;let u=V[l],y=l==="confusion",L=i.items,_=0;y&&i.items.length>J&&(_=i.items.length-J,L=i.items.slice(0,J));let B;if(y){let x=h?22:14;B=u.cy+x}else B=u.cy+(h?25:15);if([...L].forEach((x,S)=>{let C=B+S*(A+4),M=st.append("g"),N=M.append("text").attr("class","cynefinItemText").attr("x",0).attr("y",A/2).attr("text-anchor","middle").attr("dominant-baseline","central").text(x.label),$=x.label.length*7,g=N.node();if(g&&typeof g.getBBox=="function"){let O=g.getBBox();O.width>0&&($=O.width)}let w=$+ct*2,I=u.cx-w/2;M.attr("transform",`translate(${I}, ${C})`),M.insert("rect","text").attr("class","cynefinItem").attr("x",0).attr("y",0).attr("width",w).attr("height",A).attr("rx",4).attr("ry",4).attr("fill",P[l]).attr("fill-opacity",.95),N.attr("x",w/2).attr("y",A/2)}),_>0){let x=B+L.length*(A+4),S=`+${_} more`,C=st.append("g"),M=C.append("text").attr("class","cynefinItemText").attr("x",0).attr("y",A/2).attr("text-anchor","middle").attr("dominant-baseline","central").text(S),N=S.length*7,$=M.node();if($&&typeof $.getBBox=="function"){let I=$.getBBox();I.width>0&&(N=I.width)}let g=N+ct*2,w=u.cx-g/2;C.attr("transform",`translate(${w}, ${x})`),C.insert("rect","text").attr("class","cynefinItemOverflow").attr("x",0).attr("y",0).attr("width",g).attr("height",A).attr("rx",4).attr("ry",4).attr("fill",P[l]).attr("fill-opacity",.6),M.attr("x",g/2).attr("y",A/2)}}if(v.length>0){let l=k.select("defs").empty()?k.append("defs"):k.select("defs"),i=`cynefin-arrow-${e}`;l.append("marker").attr("id",i).attr("viewBox","0 0 10 10").attr("refX",9).attr("refY",5).attr("markerWidth",6).attr("markerHeight",6).attr("orient","auto-start-reverse").append("path").attr("d","M 0 0 L 10 5 L 0 10 z").attr("class","cynefinArrowHead");let u=T.append("g").attr("class","cynefin-arrows");v.forEach(y=>{let L=V[y.from],_=V[y.to];if(!L||!_)return;if(y.from===y.to){G.warn(`Cynefin renderer: skipping self-loop on domain "${y.from}"`);return}let B=L.cx,x=L.cy,S=_.cx,C=_.cy,M=(B+S)/2,N=(x+C)/2,$=S-B,g=C-x,w=Math.sqrt($*$+g*g),I=w*.15,O=-g/w,St=$/w,lt=M+O*I,dt=N+St*I;u.append("path").attr("class","cynefinArrowLine").attr("d",`M${B},${x} Q${lt},${dt} ${S},${C}`).attr("fill","none").attr("marker-end",`url(#${i})`),y.label&&u.append("text").attr("class","cynefinArrowLabel").attr("x",lt).attr("y",dt-6).attr("text-anchor","middle").attr("dominant-baseline","auto").text(y.label)})}R&&T.append("text").attr("class","cynefinTitle").attr("x",s/2).attr("y",-b/2).attr("text-anchor","middle").attr("dominant-baseline","middle").text(R)},"draw"),Vt={draw:Ht},Et=r(()=>{let t=Z(),e=X();return j(t,e.themeVariables).cynefin},"getCynefinTheme"),Gt=r(()=>{let t=Et();return`
	.cynefinDomain {
		stroke: none;
	}
	.cynefinDomainLabel {
		font-size: ${t.domainFontSize}px;
		font-weight: bold;
		fill: ${t.labelColor};
	}
	.cynefinSubtitle {
		font-size: ${t.itemFontSize-1}px;
		fill: ${t.textColor};
		font-style: italic;
	}
	.cynefinItem {
		fill-opacity: 0.95;
		stroke: ${t.boundaryColor};
		stroke-width: 1;
	}
	.cynefinItemText {
		font-size: ${t.itemFontSize}px;
		fill: ${t.textColor};
	}
	.cynefinItemOverflow {
		fill-opacity: 0.6;
		stroke: ${t.boundaryColor};
		stroke-width: 1;
		stroke-dasharray: 3 2;
	}
	.cynefinBoundary {
		stroke: ${t.boundaryColor};
		stroke-width: ${t.boundaryWidth};
		stroke-dasharray: 6 3;
	}
	.cynefinCliff {
		stroke: ${t.cliffColor};
		stroke-width: ${t.cliffWidth};
	}
	.cynefinConfusion {
		stroke: ${t.boundaryColor};
		stroke-width: 1.5;
		stroke-dasharray: 4 2;
	}
	.cynefinArrowLine {
		stroke: ${t.arrowColor};
		stroke-width: ${t.arrowWidth};
		fill: none;
	}
	.cynefinArrowHead {
		fill: ${t.arrowColor};
		stroke: none;
	}
	.cynefinArrowLabel {
		font-size: ${t.itemFontSize-1}px;
		fill: ${t.textColor};
	}
	.cynefinTitle {
		font-size: ${t.domainFontSize+2}px;
		font-weight: bold;
		fill: ${t.labelColor};
	}
	`},"styles"),Yt=Gt,he={parser:Rt,db:q,renderer:Vt,styles:Yt};export{he as diagram};
