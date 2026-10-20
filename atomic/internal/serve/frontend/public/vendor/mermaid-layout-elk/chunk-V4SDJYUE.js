import{a as B}from"./chunk-W2DK7OQN.js";import{a as W}from"./chunk-X3PD67MY.js";import"./chunk-R7U6BSD3.js";import{a as G}from"./chunk-NXUTASGL.js";import"./chunk-ECTZL2JS.js";import"./chunk-6UPRXX3O.js";import"./chunk-SHHEBKDM.js";import"./chunk-CBM6V7SV.js";import"./chunk-CESICB5C.js";import"./chunk-WUOCHPBX.js";import"./chunk-NA4UVSVP.js";import"./chunk-UNB6RQ3C.js";import"./chunk-TNB4RAFO.js";import"./chunk-TOEQHOJY.js";import"./chunk-5MPVYCPH.js";import"./chunk-NCMKR4QE.js";import"./chunk-QTWQIMGH.js";import"./chunk-IS6DLGVL.js";import"./chunk-6FNSYWGJ.js";import{o as w}from"./chunk-RANDTXFT.js";import"./chunk-PAL66KRM.js";import{O as R,T as _,U as I,V as E,W as F,X as D,Y as P,Z as z,i as k,k as O,u as A}from"./chunk-BAGAVWDO.js";import{b}from"./chunk-VYE4DNDP.js";import{a as i}from"./chunk-NKM6JPMV.js";import{d as $}from"./chunk-RPKWB5KU.js";var x={showLegend:!0,ticks:5,max:null,min:0,graticule:"circle"},C=32,V={axes:[],curves:[],options:x},g=structuredClone(V),X=O.radar,K=i(()=>w({...X,...A().radar}),"getConfig"),H=i(()=>g.axes,"getAxes"),N=i(()=>g.curves,"getCurves"),Y=i(()=>g.options,"getOptions"),Z=i(r=>{g.axes=r.map(t=>({name:t.name,label:t.label??t.name}))},"setAxes"),q=i(r=>{g.curves=r.map(t=>({name:t.name,label:t.label??t.name,entries:J(t.entries)}))},"setCurves"),J=i(r=>{if(r[0].axis==null)return r.map(e=>e.value);let t=H();if(t.length===0)throw new Error("Axes must be populated before curves for reference entries");return t.map(e=>{let a=r.find(n=>n.axis?.$refText===e.name);if(a===void 0)throw new Error("Missing entry for axis "+e.label);return a.value})},"computeCurveEntries"),Q=i(r=>{let t=r.reduce((e,a)=>(e[a.name]=a,e),{});g.options={showLegend:t.showLegend?.value??x.showLegend,ticks:t.ticks?.value??x.ticks,max:t.max?.value??x.max,min:t.min?.value??x.min,graticule:t.graticule?.value??x.graticule},g.options.ticks>C&&(b.warn(`Radar diagram ticks (${g.options.ticks}) exceeds maximum allowed (${C}). Using ${C} instead.`),g.options.ticks=C)},"setOptions"),tt=i(()=>{_(),g=structuredClone(V)},"clear"),f={getAxes:H,getCurves:N,getOptions:Y,setAxes:Z,setCurves:q,setOptions:Q,getConfig:K,clear:tt,setAccTitle:I,getAccTitle:E,setDiagramTitle:P,getDiagramTitle:z,getAccDescription:D,setAccDescription:F},et=i(r=>{B(r,f);let{axes:t,curves:e,options:a}=r;f.setAxes(t),f.setCurves(e),f.setOptions(a)},"populate"),rt={parse:i(async r=>{let t=await W("radar",r);b.debug(t),et(t)},"parse")},at=i((r,t,e,a)=>{let n=a.db,l=n.getAxes(),c=n.getCurves(),o=n.getOptions(),s=n.getConfig(),d=n.getDiagramTitle(),m=G(t),p=nt(m,s),u=o.max??Math.max(...c.map(y=>Math.max(...y.entries))),h=o.min,v=Math.min(s.width,s.height)/2;ot(p,l,v,o.ticks,o.graticule),st(p,l,v,s),M(p,l,c,h,u,o.graticule,s),S(p,c,o.showLegend,s),p.append("text").attr("class","radarTitle").text(d).attr("x",0).attr("y",-s.height/2-s.marginTop)},"draw"),nt=i((r,t)=>{let e=t.width+t.marginLeft+t.marginRight,a=t.height+t.marginTop+t.marginBottom,n={x:t.marginLeft+t.width/2,y:t.marginTop+t.height/2};return R(r,a,e,t.useMaxWidth??!0),r.attr("viewBox",`0 0 ${e} ${a}`).attr("overflow","visible"),r.append("g").attr("transform",`translate(${n.x}, ${n.y})`)},"drawFrame"),ot=i((r,t,e,a,n)=>{if(n==="circle")for(let l=0;l<a;l++){let c=e*(l+1)/a;r.append("circle").attr("r",c).attr("class","radarGraticule")}else if(n==="polygon"){let l=t.length;for(let c=0;c<a;c++){let o=e*(c+1)/a,s=t.map((d,m)=>{let p=2*m*Math.PI/l-Math.PI/2,u=o*Math.cos(p),h=o*Math.sin(p);return`${u},${h}`}).join(" ");r.append("polygon").attr("points",s).attr("class","radarGraticule")}}},"drawGraticule"),st=i((r,t,e,a)=>{let n=t.length;for(let l=0;l<n;l++){let c=t[l].label,o=2*l*Math.PI/n-Math.PI/2,s=Math.cos(o),d=Math.sin(o);r.append("line").attr("x1",0).attr("y1",0).attr("x2",e*a.axisScaleFactor*s).attr("y2",e*a.axisScaleFactor*d).attr("class","radarAxisLine");let m=s>.01?"start":s<-.01?"end":"middle",p=d>.01?"hanging":d<-.01?"auto":"central",u=4;r.append("text").text(c).attr("x",e*a.axisLabelFactor*s+u*s).attr("y",e*a.axisLabelFactor*d+u*d).attr("text-anchor",m).attr("dominant-baseline",p).attr("class","radarAxisLabel")}},"drawAxes");function M(r,t,e,a,n,l,c){let o=t.length,s=Math.min(c.width,c.height)/2;e.forEach((d,m)=>{if(d.entries.length!==o)return;let p=d.entries.map((u,h)=>{let v=2*Math.PI*h/o-Math.PI/2,y=L(u,a,n,s),j=y*Math.cos(v),U=y*Math.sin(v);return{x:j,y:U}});l==="circle"?r.append("path").attr("d",T(p,c.curveTension)).attr("class",`radarCurve-${m}`):l==="polygon"&&r.append("polygon").attr("points",p.map(u=>`${u.x},${u.y}`).join(" ")).attr("class",`radarCurve-${m}`)})}$(M,"drawCurves");i(M,"drawCurves");function L(r,t,e,a){let n=Math.min(Math.max(r,t),e);return a*(n-t)/(e-t)}$(L,"relativeRadius");i(L,"relativeRadius");function T(r,t){let e=r.length,a=`M${r[0].x},${r[0].y}`;for(let n=0;n<e;n++){let l=r[(n-1+e)%e],c=r[n],o=r[(n+1)%e],s=r[(n+2)%e],d={x:c.x+(o.x-l.x)*t,y:c.y+(o.y-l.y)*t},m={x:o.x-(s.x-c.x)*t,y:o.y-(s.y-c.y)*t};a+=` C${d.x},${d.y} ${m.x},${m.y} ${o.x},${o.y}`}return`${a} Z`}$(T,"closedRoundCurve");i(T,"closedRoundCurve");function S(r,t,e,a){if(!e)return;let n=(a.width/2+a.marginRight)*3/4,l=-(a.height/2+a.marginTop)*3/4,c=20;t.forEach((o,s)=>{let d=r.append("g").attr("transform",`translate(${n}, ${l+s*c})`);d.append("rect").attr("width",12).attr("height",12).attr("class",`radarLegendBox-${s}`),d.append("text").attr("x",16).attr("y",0).attr("class","radarLegendText").text(o.label)})}$(S,"drawLegend");i(S,"drawLegend");var it={draw:at},lt=i((r,t)=>{let e="";for(let a=0;a<r.THEME_COLOR_LIMIT;a++){let n=r[`cScale${a}`];e+=`
		.radarCurve-${a} {
			color: ${n};
			fill: ${n};
			fill-opacity: ${t.curveOpacity};
			stroke: ${n};
			stroke-width: ${t.curveStrokeWidth};
		}
		.radarLegendBox-${a} {
			fill: ${n};
			fill-opacity: ${t.curveOpacity};
			stroke: ${n};
		}
		`}return e},"genIndexStyles"),ct=i(r=>{let t=k(),e=A(),a=w(t,e.themeVariables),n=w(a.radar,r);return{themeVariables:a,radarOptions:n}},"buildRadarStyleOptions"),dt=i(({radar:r}={})=>{let{themeVariables:t,radarOptions:e}=ct(r);return`
	.radarTitle {
		font-size: ${t.fontSize};
		color: ${t.titleColor};
		dominant-baseline: hanging;
		text-anchor: middle;
	}
	.radarAxisLine {
		stroke: ${e.axisColor};
		stroke-width: ${e.axisStrokeWidth};
	}
	.radarAxisLabel {
		font-size: ${e.axisLabelFontSize}px;
		color: ${e.axisColor};
	}
	.radarGraticule {
		fill: ${e.graticuleColor};
		fill-opacity: ${e.graticuleOpacity};
		stroke: ${e.graticuleColor};
		stroke-width: ${e.graticuleStrokeWidth};
	}
	.radarLegendText {
		text-anchor: start;
		font-size: ${e.legendFontSize}px;
		dominant-baseline: hanging;
	}
	${lt(t,e)}
	`},"styles"),Dt={parser:rt,db:f,renderer:it,styles:dt};export{Dt as diagram};
