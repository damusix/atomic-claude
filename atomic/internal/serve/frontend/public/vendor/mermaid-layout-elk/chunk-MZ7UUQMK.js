import{a as ot}from"./chunk-W2DK7OQN.js";import{a as nt}from"./chunk-X3PD67MY.js";import"./chunk-R7U6BSD3.js";import{a as rt}from"./chunk-NXUTASGL.js";import"./chunk-ECTZL2JS.js";import"./chunk-6UPRXX3O.js";import"./chunk-SHHEBKDM.js";import"./chunk-CBM6V7SV.js";import"./chunk-CESICB5C.js";import"./chunk-WUOCHPBX.js";import"./chunk-NA4UVSVP.js";import"./chunk-UNB6RQ3C.js";import"./chunk-TNB4RAFO.js";import"./chunk-TOEQHOJY.js";import"./chunk-5MPVYCPH.js";import"./chunk-NCMKR4QE.js";import"./chunk-QTWQIMGH.js";import"./chunk-IS6DLGVL.js";import"./chunk-6FNSYWGJ.js";import{n as at,o as it}from"./chunk-RANDTXFT.js";import"./chunk-PAL66KRM.js";import{O as V,T as X,U as Z,V as j,W as q,X as J,Y as K,Z as Q,_ as Y,k as U}from"./chunk-BAGAVWDO.js";import{F as R,I as et,b as T,m as tt}from"./chunk-VYE4DNDP.js";import{a as l}from"./chunk-NKM6JPMV.js";import"./chunk-RPKWB5KU.js";var lt=U.pie,L={sections:new Map,showData:!1,config:lt},b=L.sections,O=L.showData,St=structuredClone(lt),xt=l(()=>structuredClone(St),"getConfig"),wt=l(()=>{b=new Map,O=L.showData,X()},"clear"),Ct=l(({label:t,value:r})=>{if(r<0)throw new Error(`"${t}" has invalid value: ${r}. Negative values are not allowed in pie charts. All slice values must be >= 0.`);b.has(t)||(b.set(t,r),T.debug(`added new section: ${t}, with value: ${r}`))},"addSection"),$t=l(()=>b,"getSections"),Dt=l(t=>{O=t},"setShowData"),yt=l(()=>O,"getShowData"),st={getConfig:xt,clear:wt,setDiagramTitle:K,getDiagramTitle:Q,setAccTitle:Z,getAccTitle:j,setAccDescription:q,getAccDescription:J,addSection:Ct,getSections:$t,setShowData:Dt,getShowData:yt},Tt=l((t,r)=>{ot(t,r),r.setShowData(t.showData),t.sections.map(r.addSection)},"populateDb"),bt={parse:l(async t=>{let r=await nt("pie",t);T.debug(r),Tt(r,st)},"parse")},At=l(t=>`
  .pieCircle{
    stroke: ${t.pieStrokeColor};
    stroke-width : ${t.pieStrokeWidth};
    opacity : ${t.pieOpacity};
  }
  .pieCircle.highlighted{
    scale: 1.05;
    opacity: 1;
  }
  .pieCircle.highlightedOnHover:hover{
    transition-duration: 250ms;
    scale: 1.05;
    opacity: 1;
  }
  .pieOuterCircle{
    stroke: ${t.pieOuterStrokeColor};
    stroke-width: ${t.pieOuterStrokeWidth};
    fill: none;
  }
  .pieTitleText {
    text-anchor: middle;
    font-size: ${t.pieTitleTextSize};
    fill: ${t.pieTitleTextColor};
    font-family: ${t.fontFamily};
  }
  .slice {
    font-family: ${t.fontFamily};
    fill: ${t.pieSectionTextColor};
    font-size:${t.pieSectionTextSize};
    // fill: white;
  }
  .legend text {
    fill: ${t.pieLegendTextColor};
    font-family: ${t.fontFamily};
    font-size: ${t.pieLegendTextSize};
  }
`,"getStyles"),kt=At,_t=l(t=>{let r=[...t.values()].reduce((n,u)=>n+u,0),W=[...t.entries()].map(([n,u])=>({label:n,value:u})).filter(n=>n.value/r*100>=1);return et().value(n=>n.value).sort(null)(W)},"createPieArcs"),zt=l((t,r,W,F)=>{T.debug(`rendering pie chart
`+t);let n=F.db,u=Y(),h=it(n.getConfig(),u.pie),H=40,i=18,c=4,S=450,x=S,A=rt(r),$=A.append("g");$.attr("transform","translate("+x/2+","+S/2+")");let{themeVariables:o}=u,[M]=at(o.pieOuterStrokeWidth);M??=2;let ct=h.legendPosition,P=h.textPosition,dt=h.donutHole>0&&h.donutHole<=.9?h.donutHole:0,m=Math.min(x,S)/2-H,pt=R().innerRadius(dt*m).outerRadius(m),gt=R().innerRadius(m*P).outerRadius(m*P),w=$.append("g");w.append("circle").attr("cx",0).attr("cy",0).attr("r",m+M/2).attr("class","pieOuterCircle");let D=n.getSections(),ht=_t(D),mt=[o.pie1,o.pie2,o.pie3,o.pie4,o.pie5,o.pie6,o.pie7,o.pie8,o.pie9,o.pie10,o.pie11,o.pie12],k=0;D.forEach(e=>{k+=e});let G=ht.filter(e=>(e.data.value/k*100).toFixed(0)!=="0"),_=tt(mt).domain([...D.keys()]);w.selectAll("mySlices").data(G).enter().append("path").attr("d",pt).attr("fill",e=>_(e.data.label)).attr("class",e=>{let a="pieCircle";return h.highlightSlice==="hover"?a+=" highlightedOnHover":h.highlightSlice===e.data.label&&(a+=" highlighted"),a}),w.selectAll("mySlices").data(G).enter().append("text").text(e=>(e.data.value/k*100).toFixed(0)+"%").attr("transform",e=>"translate("+gt.centroid(e)+")").style("text-anchor","middle").attr("class","slice");let ft=$.append("text").text(n.getDiagramTitle()).attr("x",0).attr("y",-(S-50)/2).attr("class","pieTitleText"),C=[...D.entries()].map(([e,a])=>({label:e,value:a})),f=$.selectAll(".legend").data(C).enter().append("g").attr("class","legend");f.append("rect").attr("width",i).attr("height",i).style("fill",e=>_(e.label)).style("stroke",e=>_(e.label)),f.append("text").attr("x",i+c).attr("y",i-c).text(e=>n.getShowData()?`${e.label} [${e.value}]`:e.label);let v=Math.max(...f.selectAll("text").nodes().map(e=>e?.getBoundingClientRect().width??0)),y=S,z=x+H,s=i+c,E=C.length*s;switch(ct){case"center":f.attr("transform",(e,a)=>{let d=s*C.length/2,p=-v/2-(i+c),g=a*s-d;return"translate("+p+","+g+")"});break;case"top":y+=E,f.attr("transform",(e,a)=>{let d=m,p=-v/2-(i+c),g=a*s-d;return`translate(${p}, ${g})`}),w.attr("transform",()=>`translate(0, ${E+s})`);break;case"bottom":y+=E,f.attr("transform",(e,a)=>{let d=-m-s,p=-v/2-(i+c),g=a*s-d;return"translate("+p+","+g+")"});break;case"left":z+=i+c+v,f.attr("transform",(e,a)=>{let d=s*C.length/2,p=-m-(i+c),g=a*s-d;return"translate("+p+","+g+")"}),w.attr("transform",()=>`translate(${v+i+c}, 0)`);break;default:z+=i+c+v,f.attr("transform",(e,a)=>{let d=s*C.length/2,p=12*i,g=a*s-d;return"translate("+p+","+g+")"});break}let B=ft.node()?.getBoundingClientRect().width??0,ut=x/2-B/2,vt=x/2+B/2,N=Math.min(0,ut),I=Math.max(z,vt)-N;A.attr("viewBox",`${N} 0 ${I} ${y}`),V(A,y,I,h.useMaxWidth)},"draw"),Et={draw:zt},oe={parser:bt,db:st,renderer:Et,styles:kt};export{oe as diagram};
