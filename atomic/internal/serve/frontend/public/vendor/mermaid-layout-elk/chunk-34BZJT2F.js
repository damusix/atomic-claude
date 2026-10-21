import{p as Se}from"./chunk-RANDTXFT.js";import{a as De}from"./chunk-PAL66KRM.js";import{N as se,O as ie,T as re,U as ne,V as ae,W as oe,X as ce,Y as le,Z as ue,_ as lt}from"./chunk-BAGAVWDO.js";import{A as we,B as Vt,C as Rt,D as _e,a as Lt,b as ot,d as de,e as fe,f as he,g as me,h as Tt,i as ke,o as ye,p as At,q as Ft,r as Ot,s as Wt,t as Pt,u as ge,v as pe,w as ve,x as Te,y as be,z as xe}from"./chunk-VYE4DNDP.js";import{a as l}from"./chunk-NKM6JPMV.js";import{d as E,e as vt,f as at}from"./chunk-RPKWB5KU.js";var Ve=vt({"../../node_modules/.pnpm/dayjs@1.11.21/node_modules/dayjs/plugin/isoWeek.js"(t,s){"use strict";(function(i,e){typeof t=="object"&&typeof s<"u"?s.exports=e():typeof define=="function"&&define.amd?define(e):(i=typeof globalThis<"u"?globalThis:i||self).dayjs_plugin_isoWeek=e()})(t,(function(){"use strict";var i="day";return function(e,a,g){var v=E(function(F){return F.add(4-F.isoWeekday(),i)},"a"),x=a.prototype;x.isoWeekYear=function(){return v(this).year()},x.isoWeek=function(F){if(!this.$utils().u(F))return this.add(7*(F-this.isoWeek()),i);var w,R,W,N,U=v(this),j=(w=this.isoWeekYear(),R=this.$u,W=(R?g.utc:g)().year(w).startOf("year"),N=4-W.isoWeekday(),W.isoWeekday()>4&&(N+=7),W.add(N,i));return U.diff(j,"week")+1},x.isoWeekday=function(F){return this.$utils().u(F)?this.day()||7:this.day(this.day()%7?F:F-7)};var A=x.startOf;x.startOf=function(F,w){var R=this.$utils(),W=!!R.u(w)||w;return R.p(F)==="isoweek"?W?this.date(this.date()-(this.isoWeekday()-1)).startOf("day"):this.date(this.date()-1-(this.isoWeekday()-1)+7).endOf("day"):A.bind(this)(F,w)}}}))}}),Re=vt({"../../node_modules/.pnpm/dayjs@1.11.21/node_modules/dayjs/plugin/customParseFormat.js"(t,s){"use strict";(function(i,e){typeof t=="object"&&typeof s<"u"?s.exports=e():typeof define=="function"&&define.amd?define(e):(i=typeof globalThis<"u"?globalThis:i||self).dayjs_plugin_customParseFormat=e()})(t,(function(){"use strict";var i={LTS:"h:mm:ss A",LT:"h:mm A",L:"MM/DD/YYYY",LL:"MMMM D, YYYY",LLL:"MMMM D, YYYY h:mm A",LLLL:"dddd, MMMM D, YYYY h:mm A"},e=/(\[[^[]*\])|([-_:/.,()\s]+)|(A|a|Q|YYYY|YY?|ww?|MM?M?M?|Do|DD?|hh?|HH?|mm?|ss?|S{1,3}|z|ZZ?)/g,a=/\d/,g=/\d\d/,v=/\d\d?/,x=/\d*[^-_:/,()\s\d]+/,A={},F=E(function(k){return(k=+k)+(k>68?1900:2e3)},"a"),w=E(function(k){return function(S){this[k]=+S}},"f"),R=[/[+-]\d\d:?(\d\d)?|Z/,function(k){(this.zone||(this.zone={})).offset=(function(S){if(!S||S==="Z")return 0;var P=S.match(/([+-]|\d\d)/g),$=60*P[1]+(+P[2]||0);return $===0?0:P[0]==="+"?-$:$})(k)}],W=E(function(k){var S=A[k];return S&&(S.indexOf?S:S.s.concat(S.f))},"u"),N=E(function(k,S){var P,$=A.meridiem;if($){for(var G=1;G<=24;G+=1)if(k.indexOf($(G,0,S))>-1){P=G>12;break}}else P=k===(S?"pm":"PM");return P},"d"),U={A:[x,function(k){this.afternoon=N(k,!1)}],a:[x,function(k){this.afternoon=N(k,!0)}],Q:[a,function(k){this.month=3*(k-1)+1}],S:[a,function(k){this.milliseconds=100*+k}],SS:[g,function(k){this.milliseconds=10*+k}],SSS:[/\d{3}/,function(k){this.milliseconds=+k}],s:[v,w("seconds")],ss:[v,w("seconds")],m:[v,w("minutes")],mm:[v,w("minutes")],H:[v,w("hours")],h:[v,w("hours")],HH:[v,w("hours")],hh:[v,w("hours")],D:[v,w("day")],DD:[g,w("day")],Do:[x,function(k){var S=A.ordinal,P=k.match(/\d+/);if(this.day=P[0],S)for(var $=1;$<=31;$+=1)S($).replace(/\[|\]/g,"")===k&&(this.day=$)}],w:[v,w("week")],ww:[g,w("week")],M:[v,w("month")],MM:[g,w("month")],MMM:[x,function(k){var S=W("months"),P=(W("monthsShort")||S.map((function($){return $.slice(0,3)}))).indexOf(k)+1;if(P<1)throw new Error;this.month=P%12||P}],MMMM:[x,function(k){var S=W("months").indexOf(k)+1;if(S<1)throw new Error;this.month=S%12||S}],Y:[/[+-]?\d+/,w("year")],YY:[g,function(k){this.year=F(k)}],YYYY:[/\d{4}/,w("year")],Z:R,ZZ:R};function j(k){var S,P;S=k,P=A&&A.formats;for(var $=(k=S.replace(/(\[[^\]]+])|(LTS?|l{1,4}|L{1,4})/g,(function(m,T,p){var y=p&&p.toUpperCase();return T||P[p]||i[p]||P[y].replace(/(\[[^\]]+])|(MMMM|MM|DD|dddd)/g,(function(n,d,f){return d||f.slice(1)}))}))).match(e),G=$.length,B=0;B<G;B+=1){var Y=$[B],b=U[Y],h=b&&b[0],I=b&&b[1];$[B]=I?{regex:h,parser:I}:Y.replace(/^\[|\]$/g,"")}return function(m){for(var T={},p=0,y=0;p<G;p+=1){var n=$[p];if(typeof n=="string")y+=n.length;else{var d=n.regex,f=n.parser,u=m.slice(y),_=d.exec(u)[0];f.call(T,_),m=m.replace(_,"")}}return(function(r){var C=r.afternoon;if(C!==void 0){var o=r.hours;C?o<12&&(r.hours+=12):o===12&&(r.hours=0),delete r.afternoon}})(T),T}}return E(j,"l"),function(k,S,P){P.p.customParseFormat=!0,k&&k.parseTwoDigitYear&&(F=k.parseTwoDigitYear);var $=S.prototype,G=$.parse;$.parse=function(B){var Y=B.date,b=B.utc,h=B.args;this.$u=b;var I=h[1];if(typeof I=="string"){var m=h[2]===!0,T=h[3]===!0,p=m||T,y=h[2];T&&(y=h[2]),A=this.$locale(),!m&&y&&(A=P.Ls[y]),this.$d=(function(u,_,r,C){try{if(["x","X"].indexOf(_)>-1)return new Date((_==="X"?1e3:1)*u);var o=j(_)(u),H=o.year,c=o.month,D=o.day,M=o.hours,V=o.minutes,z=o.seconds,L=o.milliseconds,O=o.zone,rt=o.week,et=new Date,pt=D||(H||c?1:et.getDate()),dt=H||et.getFullYear(),nt=0;H&&!c||(nt=c>0?c-1:et.getMonth());var q,ct=M||0,K=V||0,Z=z||0,wt=L||0;return O?new Date(Date.UTC(dt,nt,pt,ct,K,Z,wt+60*O.offset*1e3)):r?new Date(Date.UTC(dt,nt,pt,ct,K,Z,wt)):(q=new Date(dt,nt,pt,ct,K,Z,wt),rt&&(q=C(q).week(rt).toDate()),q)}catch{return new Date("")}})(Y,I,b,P),this.init(),y&&y!==!0&&(this.$L=this.locale(y).$L),p&&Y!=this.format(I)&&(this.$d=new Date("")),A={}}else if(I instanceof Array)for(var n=I.length,d=1;d<=n;d+=1){h[1]=I[d-1];var f=P.apply(this,h);if(f.isValid()){this.$d=f.$d,this.$L=f.$L,this.init();break}d===n&&(this.$d=new Date(""))}else G.call(this,B)}}}))}}),ze=vt({"../../node_modules/.pnpm/dayjs@1.11.21/node_modules/dayjs/plugin/advancedFormat.js"(t,s){"use strict";(function(i,e){typeof t=="object"&&typeof s<"u"?s.exports=e():typeof define=="function"&&define.amd?define(e):(i=typeof globalThis<"u"?globalThis:i||self).dayjs_plugin_advancedFormat=e()})(t,(function(){"use strict";return function(i,e){var a=e.prototype,g=a.format;a.format=function(v){var x=this,A=this.$locale();if(!this.isValid())return g.bind(this)(v);var F=this.$utils(),w=(v||"YYYY-MM-DDTHH:mm:ssZ").replace(/\[([^\]]+)]|Q|wo|ww|w|WW|W|zzz|z|gggg|GGGG|Do|X|x|k{1,2}|S/g,(function(R){switch(R){case"Q":return Math.ceil((x.$M+1)/3);case"Do":return A.ordinal(x.$D);case"gggg":return x.weekYear();case"GGGG":return x.isoWeekYear();case"wo":return A.ordinal(x.week(),"W");case"w":case"ww":return F.s(x.week(),R==="w"?1:2,"0");case"W":case"WW":return F.s(x.isoWeek(),R==="W"?1:2,"0");case"k":case"kk":return F.s(String(x.$H===0?24:x.$H),R==="k"?1:2,"0");case"X":return Math.floor(x.$d.getTime()/1e3);case"x":return x.$d.getTime();case"z":return"["+x.offsetName()+"]";case"zzz":return"["+x.offsetName("long")+"]";default:return R}}));return g.bind(this)(w)}}}))}}),Ne=vt({"../../node_modules/.pnpm/dayjs@1.11.21/node_modules/dayjs/plugin/duration.js"(t,s){"use strict";(function(i,e){typeof t=="object"&&typeof s<"u"?s.exports=e():typeof define=="function"&&define.amd?define(e):(i=typeof globalThis<"u"?globalThis:i||self).dayjs_plugin_duration=e()})(t,(function(){"use strict";var i,e,a=1e3,g=6e4,v=36e5,x=864e5,A=31536e6,F=2628e6,w=/^(-|\+)?P(?:([-+]?[0-9,.]*)Y)?(?:([-+]?[0-9,.]*)M)?(?:([-+]?[0-9,.]*)W)?(?:([-+]?[0-9,.]*)D)?(?:T(?:([-+]?[0-9,.]*)H)?(?:([-+]?[0-9,.]*)M)?(?:([-+]?[0-9,.]*)S)?)?$/,R=/\[([^\]]+)]|YYYY|YY|Y|M{1,2}|D{1,2}|H{1,2}|m{1,2}|s{1,2}|SSS/g,W={years:A,months:F,days:x,hours:v,minutes:g,seconds:a,milliseconds:1,weeks:6048e5},N=E(function(Y){return Y instanceof G},"c"),U=E(function(Y,b,h){return new G(Y,h,b.$l)},"f"),j=E(function(Y){return e.p(Y)+"s"},"m"),k=E(function(Y){return Y<0},"l"),S=E(function(Y){return k(Y)?Math.ceil(Y):Math.floor(Y)},"$"),P=E(function(Y){return Math.abs(Y)},"y"),$=E(function(Y,b){return Y?k(Y)?{negative:!0,format:""+P(Y)+b}:{negative:!1,format:""+Y+b}:{negative:!1,format:""}},"v"),G=(function(){function Y(h,I,m){var T=this;if(this.$d={},this.$l=m,h===void 0&&(this.$ms=0,this.parseFromMilliseconds()),I)return U(h*W[j(I)],this);if(typeof h=="number")return this.$ms=h,this.parseFromMilliseconds(),this;if(typeof h=="object")return Object.keys(h).forEach((function(n){T.$d[j(n)]=h[n]})),this.calMilliseconds(),this;if(typeof h=="string"){var p=h.match(w);if(p){var y=p.slice(2).map((function(n){return n!=null?Number(n):0}));return this.$d.years=y[0],this.$d.months=y[1],this.$d.weeks=y[2],this.$d.days=y[3],this.$d.hours=y[4],this.$d.minutes=y[5],this.$d.seconds=y[6],this.calMilliseconds(),this}}return this}E(Y,"l");var b=Y.prototype;return b.calMilliseconds=function(){var h=this;this.$ms=Object.keys(this.$d).reduce((function(I,m){return I+(h.$d[m]||0)*W[m]}),0)},b.parseFromMilliseconds=function(){var h=this.$ms;this.$d.years=S(h/A),h%=A,this.$d.months=S(h/F),h%=F,this.$d.days=S(h/x),h%=x,this.$d.hours=S(h/v),h%=v,this.$d.minutes=S(h/g),h%=g,this.$d.seconds=S(h/a),h%=a,this.$d.milliseconds=h},b.toISOString=function(){var h=$(this.$d.years,"Y"),I=$(this.$d.months,"M"),m=+this.$d.days||0;this.$d.weeks&&(m+=7*this.$d.weeks);var T=$(m,"D"),p=$(this.$d.hours,"H"),y=$(this.$d.minutes,"M"),n=this.$d.seconds||0;this.$d.milliseconds&&(n+=this.$d.milliseconds/1e3,n=Math.round(1e3*n)/1e3);var d=$(n,"S"),f=h.negative||I.negative||T.negative||p.negative||y.negative||d.negative,u=p.format||y.format||d.format?"T":"",_=(f?"-":"")+"P"+h.format+I.format+T.format+u+p.format+y.format+d.format;return _==="P"||_==="-P"?"P0D":_},b.toJSON=function(){return this.toISOString()},b.format=function(h){var I=h||"YYYY-MM-DDTHH:mm:ss",m={Y:this.$d.years,YY:e.s(this.$d.years,2,"0"),YYYY:e.s(this.$d.years,4,"0"),M:this.$d.months,MM:e.s(this.$d.months,2,"0"),D:this.$d.days,DD:e.s(this.$d.days,2,"0"),H:this.$d.hours,HH:e.s(this.$d.hours,2,"0"),m:this.$d.minutes,mm:e.s(this.$d.minutes,2,"0"),s:this.$d.seconds,ss:e.s(this.$d.seconds,2,"0"),SSS:e.s(this.$d.milliseconds,3,"0")};return I.replace(R,(function(T,p){return p||String(m[T])}))},b.as=function(h){return this.$ms/W[j(h)]},b.get=function(h){var I=this.$ms,m=j(h);return m==="milliseconds"?I%=1e3:I=m==="weeks"?S(I/W[m]):this.$d[m],I||0},b.add=function(h,I,m){var T;return T=I?h*W[j(I)]:N(h)?h.$ms:U(h,this).$ms,U(this.$ms+T*(m?-1:1),this)},b.subtract=function(h,I){return this.add(h,I,!0)},b.locale=function(h){var I=this.clone();return I.$l=h,I},b.clone=function(){return U(this.$ms,this)},b.humanize=function(h){return i().add(this.$ms,"ms").locale(this.$l).fromNow(!h)},b.valueOf=function(){return this.asMilliseconds()},b.milliseconds=function(){return this.get("milliseconds")},b.asMilliseconds=function(){return this.as("milliseconds")},b.seconds=function(){return this.get("seconds")},b.asSeconds=function(){return this.as("seconds")},b.minutes=function(){return this.get("minutes")},b.asMinutes=function(){return this.as("minutes")},b.hours=function(){return this.get("hours")},b.asHours=function(){return this.as("hours")},b.days=function(){return this.get("days")},b.asDays=function(){return this.as("days")},b.weeks=function(){return this.get("weeks")},b.asWeeks=function(){return this.as("weeks")},b.months=function(){return this.get("months")},b.asMonths=function(){return this.as("months")},b.years=function(){return this.get("years")},b.asYears=function(){return this.as("years")},Y})(),B=E(function(Y,b,h){return Y.add(b.years()*h,"y").add(b.months()*h,"M").add(b.days()*h,"d").add(b.hours()*h,"h").add(b.minutes()*h,"m").add(b.seconds()*h,"s").add(b.milliseconds()*h,"ms")},"p");return function(Y,b,h){i=h,e=h().$utils(),h.duration=function(T,p){var y=h.locale();return U(T,{$l:y},p)},h.isDuration=N;var I=b.prototype.add,m=b.prototype.subtract;b.prototype.add=function(T,p){return N(T)?B(this,T,1):I.bind(this)(T,p)},b.prototype.subtract=function(T,p){return N(T)?B(this,T,-1):m.bind(this)(T,p)}}}))}}),je=at(De(),1),J=at(Lt(),1),He=at(Ve(),1),Be=at(Re(),1),qe=at(ze(),1),bt=at(Lt(),1),Ge=at(Ne(),1),Nt=(function(){var t=l(function(y,n,d,f){for(d=d||{},f=y.length;f--;d[y[f]]=n);return d},"o"),s=[6,8,10,12,13,14,15,16,17,18,20,21,22,23,24,25,26,27,28,29,30,31,33,35,36,38,40],i=[1,26],e=[1,27],a=[1,28],g=[1,29],v=[1,30],x=[1,31],A=[1,32],F=[1,33],w=[1,34],R=[1,9],W=[1,10],N=[1,11],U=[1,12],j=[1,13],k=[1,14],S=[1,15],P=[1,16],$=[1,19],G=[1,20],B=[1,21],Y=[1,22],b=[1,23],h=[1,25],I=[1,35],m={trace:l(E(function(){},"trace"),"trace"),yy:{},symbols_:{error:2,start:3,gantt:4,document:5,EOF:6,line:7,SPACE:8,statement:9,NL:10,weekday:11,weekday_monday:12,weekday_tuesday:13,weekday_wednesday:14,weekday_thursday:15,weekday_friday:16,weekday_saturday:17,weekday_sunday:18,weekend:19,weekend_friday:20,weekend_saturday:21,dateFormat:22,inclusiveEndDates:23,topAxis:24,axisFormat:25,tickInterval:26,excludes:27,includes:28,todayMarker:29,title:30,acc_title:31,acc_title_value:32,acc_descr:33,acc_descr_value:34,acc_descr_multiline_value:35,section:36,clickStatement:37,taskTxt:38,taskData:39,click:40,callbackname:41,callbackargs:42,href:43,clickStatementDebug:44,$accept:0,$end:1},terminals_:{2:"error",4:"gantt",6:"EOF",8:"SPACE",10:"NL",12:"weekday_monday",13:"weekday_tuesday",14:"weekday_wednesday",15:"weekday_thursday",16:"weekday_friday",17:"weekday_saturday",18:"weekday_sunday",20:"weekend_friday",21:"weekend_saturday",22:"dateFormat",23:"inclusiveEndDates",24:"topAxis",25:"axisFormat",26:"tickInterval",27:"excludes",28:"includes",29:"todayMarker",30:"title",31:"acc_title",32:"acc_title_value",33:"acc_descr",34:"acc_descr_value",35:"acc_descr_multiline_value",36:"section",38:"taskTxt",39:"taskData",40:"click",41:"callbackname",42:"callbackargs",43:"href"},productions_:[0,[3,3],[5,0],[5,2],[7,2],[7,1],[7,1],[7,1],[11,1],[11,1],[11,1],[11,1],[11,1],[11,1],[11,1],[19,1],[19,1],[9,1],[9,1],[9,1],[9,1],[9,1],[9,1],[9,1],[9,1],[9,1],[9,1],[9,1],[9,2],[9,2],[9,1],[9,1],[9,1],[9,2],[37,2],[37,3],[37,3],[37,4],[37,3],[37,4],[37,2],[44,2],[44,3],[44,3],[44,4],[44,3],[44,4],[44,2]],performAction:l(E(function(n,d,f,u,_,r,C){var o=r.length-1;switch(_){case 1:return r[o-1];case 2:this.$=[];break;case 3:r[o-1].push(r[o]),this.$=r[o-1];break;case 4:case 5:this.$=r[o];break;case 6:case 7:this.$=[];break;case 8:u.setWeekday("monday");break;case 9:u.setWeekday("tuesday");break;case 10:u.setWeekday("wednesday");break;case 11:u.setWeekday("thursday");break;case 12:u.setWeekday("friday");break;case 13:u.setWeekday("saturday");break;case 14:u.setWeekday("sunday");break;case 15:u.setWeekend("friday");break;case 16:u.setWeekend("saturday");break;case 17:u.setDateFormat(r[o].substr(11)),this.$=r[o].substr(11);break;case 18:u.enableInclusiveEndDates(),this.$=r[o].substr(18);break;case 19:u.TopAxis(),this.$=r[o].substr(8);break;case 20:u.setAxisFormat(r[o].substr(11)),this.$=r[o].substr(11);break;case 21:u.setTickInterval(r[o].substr(13)),this.$=r[o].substr(13);break;case 22:u.setExcludes(r[o].substr(9)),this.$=r[o].substr(9);break;case 23:u.setIncludes(r[o].substr(9)),this.$=r[o].substr(9);break;case 24:u.setTodayMarker(r[o].substr(12)),this.$=r[o].substr(12);break;case 27:u.setDiagramTitle(r[o].substr(6)),this.$=r[o].substr(6);break;case 28:this.$=r[o].trim(),u.setAccTitle(this.$);break;case 29:case 30:this.$=r[o].trim(),u.setAccDescription(this.$);break;case 31:u.addSection(r[o].substr(8)),this.$=r[o].substr(8);break;case 33:u.addTask(r[o-1],r[o]),this.$="task";break;case 34:this.$=r[o-1],u.setClickEvent(r[o-1],r[o],null);break;case 35:this.$=r[o-2],u.setClickEvent(r[o-2],r[o-1],r[o]);break;case 36:this.$=r[o-2],u.setClickEvent(r[o-2],r[o-1],null),u.setLink(r[o-2],r[o]);break;case 37:this.$=r[o-3],u.setClickEvent(r[o-3],r[o-2],r[o-1]),u.setLink(r[o-3],r[o]);break;case 38:this.$=r[o-2],u.setClickEvent(r[o-2],r[o],null),u.setLink(r[o-2],r[o-1]);break;case 39:this.$=r[o-3],u.setClickEvent(r[o-3],r[o-1],r[o]),u.setLink(r[o-3],r[o-2]);break;case 40:this.$=r[o-1],u.setLink(r[o-1],r[o]);break;case 41:case 47:this.$=r[o-1]+" "+r[o];break;case 42:case 43:case 45:this.$=r[o-2]+" "+r[o-1]+" "+r[o];break;case 44:case 46:this.$=r[o-3]+" "+r[o-2]+" "+r[o-1]+" "+r[o];break}},"anonymous"),"anonymous"),table:[{3:1,4:[1,2]},{1:[3]},t(s,[2,2],{5:3}),{6:[1,4],7:5,8:[1,6],9:7,10:[1,8],11:17,12:i,13:e,14:a,15:g,16:v,17:x,18:A,19:18,20:F,21:w,22:R,23:W,24:N,25:U,26:j,27:k,28:S,29:P,30:$,31:G,33:B,35:Y,36:b,37:24,38:h,40:I},t(s,[2,7],{1:[2,1]}),t(s,[2,3]),{9:36,11:17,12:i,13:e,14:a,15:g,16:v,17:x,18:A,19:18,20:F,21:w,22:R,23:W,24:N,25:U,26:j,27:k,28:S,29:P,30:$,31:G,33:B,35:Y,36:b,37:24,38:h,40:I},t(s,[2,5]),t(s,[2,6]),t(s,[2,17]),t(s,[2,18]),t(s,[2,19]),t(s,[2,20]),t(s,[2,21]),t(s,[2,22]),t(s,[2,23]),t(s,[2,24]),t(s,[2,25]),t(s,[2,26]),t(s,[2,27]),{32:[1,37]},{34:[1,38]},t(s,[2,30]),t(s,[2,31]),t(s,[2,32]),{39:[1,39]},t(s,[2,8]),t(s,[2,9]),t(s,[2,10]),t(s,[2,11]),t(s,[2,12]),t(s,[2,13]),t(s,[2,14]),t(s,[2,15]),t(s,[2,16]),{41:[1,40],43:[1,41]},t(s,[2,4]),t(s,[2,28]),t(s,[2,29]),t(s,[2,33]),t(s,[2,34],{42:[1,42],43:[1,43]}),t(s,[2,40],{41:[1,44]}),t(s,[2,35],{43:[1,45]}),t(s,[2,36]),t(s,[2,38],{42:[1,46]}),t(s,[2,37]),t(s,[2,39])],defaultActions:{},parseError:l(E(function(n,d){if(d.recoverable)this.trace(n);else{var f=new Error(n);throw f.hash=d,f}},"parseError"),"parseError"),parse:l(E(function(n){var d=this,f=[0],u=[],_=[null],r=[],C=this.table,o="",H=0,c=0,D=0,M=2,V=1,z=r.slice.call(arguments,1),L=Object.create(this.lexer),O={yy:{}};for(var rt in this.yy)Object.prototype.hasOwnProperty.call(this.yy,rt)&&(O.yy[rt]=this.yy[rt]);L.setInput(n,O.yy),O.yy.lexer=L,O.yy.parser=this,typeof L.yylloc>"u"&&(L.yylloc={});var et=L.yylloc;r.push(et);var pt=L.options&&L.options.ranges;typeof O.yy.parseError=="function"?this.parseError=O.yy.parseError:this.parseError=Object.getPrototypeOf(this).parseError;function dt(Q){f.length=f.length-2*Q,_.length=_.length-Q,r.length=r.length-Q}E(dt,"popStack"),l(dt,"popStack");function nt(){var Q;return Q=u.pop()||L.lex()||V,typeof Q!="number"&&(Q instanceof Array&&(u=Q,Q=u.pop()),Q=d.symbols_[Q]||Q),Q}E(nt,"lex"),l(nt,"lex");for(var q,ct,K,Z,wt,_t,ft={},Dt,st,ee,St;;){if(K=f[f.length-1],this.defaultActions[K]?Z=this.defaultActions[K]:((q===null||typeof q>"u")&&(q=nt()),Z=C[K]&&C[K][q]),typeof Z>"u"||!Z.length||!Z[0]){var $t="";St=[];for(Dt in C[K])this.terminals_[Dt]&&Dt>M&&St.push("'"+this.terminals_[Dt]+"'");L.showPosition?$t="Parse error on line "+(H+1)+`:
`+L.showPosition()+`
Expecting `+St.join(", ")+", got '"+(this.terminals_[q]||q)+"'":$t="Parse error on line "+(H+1)+": Unexpected "+(q==V?"end of input":"'"+(this.terminals_[q]||q)+"'"),this.parseError($t,{text:L.match,token:this.terminals_[q]||q,line:L.yylineno,loc:et,expected:St})}if(Z[0]instanceof Array&&Z.length>1)throw new Error("Parse Error: multiple actions possible at state: "+K+", token: "+q);switch(Z[0]){case 1:f.push(q),_.push(L.yytext),r.push(L.yylloc),f.push(Z[1]),q=null,ct?(q=ct,ct=null):(c=L.yyleng,o=L.yytext,H=L.yylineno,et=L.yylloc,D>0&&D--);break;case 2:if(st=this.productions_[Z[1]][1],ft.$=_[_.length-st],ft._$={first_line:r[r.length-(st||1)].first_line,last_line:r[r.length-1].last_line,first_column:r[r.length-(st||1)].first_column,last_column:r[r.length-1].last_column},pt&&(ft._$.range=[r[r.length-(st||1)].range[0],r[r.length-1].range[1]]),_t=this.performAction.apply(ft,[o,c,H,O.yy,Z[1],_,r].concat(z)),typeof _t<"u")return _t;st&&(f=f.slice(0,-1*st*2),_=_.slice(0,-1*st),r=r.slice(0,-1*st)),f.push(this.productions_[Z[1]][0]),_.push(ft.$),r.push(ft._$),ee=C[f[f.length-2]][f[f.length-1]],f.push(ee);break;case 3:return!0}}return!0},"parse"),"parse")},T=(function(){var y={EOF:1,parseError:l(E(function(d,f){if(this.yy.parser)this.yy.parser.parseError(d,f);else throw new Error(d)},"parseError"),"parseError"),setInput:l(function(n,d){return this.yy=d||this.yy||{},this._input=n,this._more=this._backtrack=this.done=!1,this.yylineno=this.yyleng=0,this.yytext=this.matched=this.match="",this.conditionStack=["INITIAL"],this.yylloc={first_line:1,first_column:0,last_line:1,last_column:0},this.options.ranges&&(this.yylloc.range=[0,0]),this.offset=0,this},"setInput"),input:l(function(){var n=this._input[0];this.yytext+=n,this.yyleng++,this.offset++,this.match+=n,this.matched+=n;var d=n.match(/(?:\r\n?|\n).*/g);return d?(this.yylineno++,this.yylloc.last_line++):this.yylloc.last_column++,this.options.ranges&&this.yylloc.range[1]++,this._input=this._input.slice(1),n},"input"),unput:l(function(n){var d=n.length,f=n.split(/(?:\r\n?|\n)/g);this._input=n+this._input,this.yytext=this.yytext.substr(0,this.yytext.length-d),this.offset-=d;var u=this.match.split(/(?:\r\n?|\n)/g);this.match=this.match.substr(0,this.match.length-1),this.matched=this.matched.substr(0,this.matched.length-1),f.length-1&&(this.yylineno-=f.length-1);var _=this.yylloc.range;return this.yylloc={first_line:this.yylloc.first_line,last_line:this.yylineno+1,first_column:this.yylloc.first_column,last_column:f?(f.length===u.length?this.yylloc.first_column:0)+u[u.length-f.length].length-f[0].length:this.yylloc.first_column-d},this.options.ranges&&(this.yylloc.range=[_[0],_[0]+this.yyleng-d]),this.yyleng=this.yytext.length,this},"unput"),more:l(function(){return this._more=!0,this},"more"),reject:l(function(){if(this.options.backtrack_lexer)this._backtrack=!0;else return this.parseError("Lexical error on line "+(this.yylineno+1)+`. You can only invoke reject() in the lexer when the lexer is of the backtracking persuasion (options.backtrack_lexer = true).
`+this.showPosition(),{text:"",token:null,line:this.yylineno});return this},"reject"),less:l(function(n){this.unput(this.match.slice(n))},"less"),pastInput:l(function(){var n=this.matched.substr(0,this.matched.length-this.match.length);return(n.length>20?"...":"")+n.substr(-20).replace(/\n/g,"")},"pastInput"),upcomingInput:l(function(){var n=this.match;return n.length<20&&(n+=this._input.substr(0,20-n.length)),(n.substr(0,20)+(n.length>20?"...":"")).replace(/\n/g,"")},"upcomingInput"),showPosition:l(function(){var n=this.pastInput(),d=new Array(n.length+1).join("-");return n+this.upcomingInput()+`
`+d+"^"},"showPosition"),test_match:l(function(n,d){var f,u,_;if(this.options.backtrack_lexer&&(_={yylineno:this.yylineno,yylloc:{first_line:this.yylloc.first_line,last_line:this.last_line,first_column:this.yylloc.first_column,last_column:this.yylloc.last_column},yytext:this.yytext,match:this.match,matches:this.matches,matched:this.matched,yyleng:this.yyleng,offset:this.offset,_more:this._more,_input:this._input,yy:this.yy,conditionStack:this.conditionStack.slice(0),done:this.done},this.options.ranges&&(_.yylloc.range=this.yylloc.range.slice(0))),u=n[0].match(/(?:\r\n?|\n).*/g),u&&(this.yylineno+=u.length),this.yylloc={first_line:this.yylloc.last_line,last_line:this.yylineno+1,first_column:this.yylloc.last_column,last_column:u?u[u.length-1].length-u[u.length-1].match(/\r?\n?/)[0].length:this.yylloc.last_column+n[0].length},this.yytext+=n[0],this.match+=n[0],this.matches=n,this.yyleng=this.yytext.length,this.options.ranges&&(this.yylloc.range=[this.offset,this.offset+=this.yyleng]),this._more=!1,this._backtrack=!1,this._input=this._input.slice(n[0].length),this.matched+=n[0],f=this.performAction.call(this,this.yy,this,d,this.conditionStack[this.conditionStack.length-1]),this.done&&this._input&&(this.done=!1),f)return f;if(this._backtrack){for(var r in _)this[r]=_[r];return!1}return!1},"test_match"),next:l(function(){if(this.done)return this.EOF;this._input||(this.done=!0);var n,d,f,u;this._more||(this.yytext="",this.match="");for(var _=this._currentRules(),r=0;r<_.length;r++)if(f=this._input.match(this.rules[_[r]]),f&&(!d||f[0].length>d[0].length)){if(d=f,u=r,this.options.backtrack_lexer){if(n=this.test_match(f,_[r]),n!==!1)return n;if(this._backtrack){d=!1;continue}else return!1}else if(!this.options.flex)break}return d?(n=this.test_match(d,_[u]),n!==!1?n:!1):this._input===""?this.EOF:this.parseError("Lexical error on line "+(this.yylineno+1)+`. Unrecognized text.
`+this.showPosition(),{text:"",token:null,line:this.yylineno})},"next"),lex:l(E(function(){var d=this.next();return d||this.lex()},"lex"),"lex"),begin:l(E(function(d){this.conditionStack.push(d)},"begin"),"begin"),popState:l(E(function(){var d=this.conditionStack.length-1;return d>0?this.conditionStack.pop():this.conditionStack[0]},"popState"),"popState"),_currentRules:l(E(function(){return this.conditionStack.length&&this.conditionStack[this.conditionStack.length-1]?this.conditions[this.conditionStack[this.conditionStack.length-1]].rules:this.conditions.INITIAL.rules},"_currentRules"),"_currentRules"),topState:l(E(function(d){return d=this.conditionStack.length-1-Math.abs(d||0),d>=0?this.conditionStack[d]:"INITIAL"},"topState"),"topState"),pushState:l(E(function(d){this.begin(d)},"pushState"),"pushState"),stateStackSize:l(E(function(){return this.conditionStack.length},"stateStackSize"),"stateStackSize"),options:{"case-insensitive":!0},performAction:l(E(function(d,f,u,_){var r=_;switch(u){case 0:return this.begin("open_directive"),"open_directive";break;case 1:return this.begin("acc_title"),31;break;case 2:return this.popState(),"acc_title_value";break;case 3:return this.begin("acc_descr"),33;break;case 4:return this.popState(),"acc_descr_value";break;case 5:this.begin("acc_descr_multiline");break;case 6:this.popState();break;case 7:return"acc_descr_multiline_value";case 8:break;case 9:break;case 10:break;case 11:return 10;case 12:break;case 13:break;case 14:this.begin("href");break;case 15:this.popState();break;case 16:return 43;case 17:this.begin("callbackname");break;case 18:this.popState();break;case 19:this.popState(),this.begin("callbackargs");break;case 20:return 41;case 21:this.popState();break;case 22:return 42;case 23:this.begin("click");break;case 24:this.popState();break;case 25:return 40;case 26:return 4;case 27:return 22;case 28:return 23;case 29:return 24;case 30:return 25;case 31:return 26;case 32:return 28;case 33:return 27;case 34:return 29;case 35:return 12;case 36:return 13;case 37:return 14;case 38:return 15;case 39:return 16;case 40:return 17;case 41:return 18;case 42:return 20;case 43:return 21;case 44:return"date";case 45:return 30;case 46:return"accDescription";case 47:return 36;case 48:return 38;case 49:return 39;case 50:return":";case 51:return 6;case 52:return"INVALID"}},"anonymous"),"anonymous"),rules:[/^(?:%%\{)/i,/^(?:accTitle\s*:\s*)/i,/^(?:(?!\n||)*[^\n]*)/i,/^(?:accDescr\s*:\s*)/i,/^(?:(?!\n||)*[^\n]*)/i,/^(?:accDescr\s*\{\s*)/i,/^(?:[\}])/i,/^(?:[^\}]*)/i,/^(?:%%(?!\{)*[^\n]*)/i,/^(?:[^\}]%%*[^\n]*)/i,/^(?:%%*[^\n]*[\n]*)/i,/^(?:[\n]+)/i,/^(?:\s+)/i,/^(?:%[^\n]*)/i,/^(?:href[\s]+["])/i,/^(?:["])/i,/^(?:[^"]*)/i,/^(?:call[\s]+)/i,/^(?:\([\s]*\))/i,/^(?:\()/i,/^(?:[^(]*)/i,/^(?:\))/i,/^(?:[^)]*)/i,/^(?:click[\s]+)/i,/^(?:[\s\n])/i,/^(?:[^\s\n]*)/i,/^(?:gantt\b)/i,/^(?:dateFormat\s[^#\n;]+)/i,/^(?:inclusiveEndDates\b)/i,/^(?:topAxis\b)/i,/^(?:axisFormat\s[^#\n;]+)/i,/^(?:tickInterval\s[^#\n;]+)/i,/^(?:includes\s[^#\n;]+)/i,/^(?:excludes\s[^#\n;]+)/i,/^(?:todayMarker\s[^\n;]+)/i,/^(?:weekday\s+monday\b)/i,/^(?:weekday\s+tuesday\b)/i,/^(?:weekday\s+wednesday\b)/i,/^(?:weekday\s+thursday\b)/i,/^(?:weekday\s+friday\b)/i,/^(?:weekday\s+saturday\b)/i,/^(?:weekday\s+sunday\b)/i,/^(?:weekend\s+friday\b)/i,/^(?:weekend\s+saturday\b)/i,/^(?:\d\d\d\d-\d\d-\d\d\b)/i,/^(?:title\s[^\n]+)/i,/^(?:accDescription\s[^#\n;]+)/i,/^(?:section\s[^\n]+)/i,/^(?:[^:\n]+)/i,/^(?::[^#\n;]+)/i,/^(?::)/i,/^(?:$)/i,/^(?:.)/i],conditions:{acc_descr_multiline:{rules:[6,7],inclusive:!1},acc_descr:{rules:[4],inclusive:!1},acc_title:{rules:[2],inclusive:!1},callbackargs:{rules:[21,22],inclusive:!1},callbackname:{rules:[18,19,20],inclusive:!1},href:{rules:[15,16],inclusive:!1},click:{rules:[24,25],inclusive:!1},INITIAL:{rules:[0,1,3,5,8,9,10,11,12,13,14,17,23,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51,52],inclusive:!0}}};return y})();m.lexer=T;function p(){this.yy={}}return E(p,"Parser"),l(p,"Parser"),p.prototype=m,m.Parser=p,new p})();Nt.parser=Nt;var Xe=Nt;J.default.extend(He.default);J.default.extend(Be.default);J.default.extend(qe.default);var Ce={friday:5,saturday:6},tt="",qt="",Gt=void 0,Xt="",kt=[],yt=[],Ut=new Map,Zt=[],Et=[],gt="",Qt="",Ie=["active","done","crit","milestone","vert"],Jt=[],ht="",xt=!1,Kt=!1,te="sunday",It="saturday",jt=0,Ue=l(function(){Zt=[],Et=[],gt="",Jt=[],Ct=0,Bt=void 0,Mt=void 0,X=[],tt="",qt="",Qt="",Gt=void 0,Xt="",kt=[],yt=[],xt=!1,Kt=!1,jt=0,Ut=new Map,ht="",re(),te="sunday",It="saturday"},"clear"),Ze=l(function(t){ht=t},"setDiagramId"),Qe=l(function(t){qt=t},"setAxisFormat"),Je=l(function(){return qt},"getAxisFormat"),Ke=l(function(t){Gt=t},"setTickInterval"),ts=l(function(){return Gt},"getTickInterval"),es=l(function(t){Xt=t},"setTodayMarker"),ss=l(function(){return Xt},"getTodayMarker"),is=l(function(t){tt=t},"setDateFormat"),rs=l(function(){xt=!0},"enableInclusiveEndDates"),ns=l(function(){return xt},"endDatesAreInclusive"),as=l(function(){Kt=!0},"enableTopAxis"),os=l(function(){return Kt},"topAxisEnabled"),cs=l(function(t){Qt=t},"setDisplayMode"),ls=l(function(){return Qt},"getDisplayMode"),us=l(function(){return tt},"getDateFormat"),Ye=l((t,s)=>{let i=s.toLowerCase().split(/[\s,]+/).filter(e=>e!=="");return[...new Set([...t,...i])]},"mergeTokens"),ds=l(function(t){kt=Ye(kt,t)},"setIncludes"),fs=l(function(){return kt},"getIncludes"),hs=l(function(t){yt=Ye(yt,t)},"setExcludes"),ms=l(function(){return yt},"getExcludes"),ks=l(function(){return Ut},"getLinks"),ys=l(function(t){gt=t,Zt.push(t)},"addSection"),gs=l(function(){return Zt},"getSections"),ps=l(function(){let t=Me(),s=10,i=0;for(;!t&&i<s;)t=Me(),i++;return Et=X,Et},"getTasks"),$e=l(function(t,s,i,e){let a=t.format(s.trim()),g=t.format("YYYY-MM-DD");return e.includes(a)||e.includes(g)?!1:i.includes("weekends")&&(t.isoWeekday()===Ce[It]||t.isoWeekday()===Ce[It]+1)||i.includes(t.format("dddd").toLowerCase())?!0:i.includes(a)||i.includes(g)},"isInvalidDate"),vs=l(function(t){te=t},"setWeekday"),Ts=l(function(){return te},"getWeekday"),bs=l(function(t){It=t},"setWeekend"),Le=l(function(t,s,i,e){if(!i.length||t.manualEndTime)return;let a;t.startTime instanceof Date?a=(0,J.default)(t.startTime):a=(0,J.default)(t.startTime,s,!0),a=a.add(1,"d");let g;t.endTime instanceof Date?g=(0,J.default)(t.endTime):g=(0,J.default)(t.endTime,s,!0);let[v,x]=xs(a,g,s,i,e);t.endTime=v.toDate(),t.renderEndTime=x},"checkTaskDates"),xs=l(function(t,s,i,e,a){let g=!1,v=null,x=s.add(1e4,"d");for(;t<=s;){if(g||(v=s.toDate()),g=$e(t,i,e,a),g&&(s=s.add(1,"d"),s>x))throw new Error("Failed to find a valid date that was not excluded by `excludes` after 10,000 iterations.");t=t.add(1,"d")}return[s,v]},"fixTaskDates"),Ht=l(function(t,s,i){if(i=i.trim(),l(x=>{let A=x.trim();return A==="x"||A==="X"},"isTimestampFormat")(s)&&/^\d+$/.test(i))return new Date(Number(i));let g=/^after\s+(?<ids>[\d\w- ]+)/.exec(i);if(g!==null){let x=null;for(let F of g.groups.ids.split(" ")){let w=ut(F);w!==void 0&&(!x||w.endTime>x.endTime)&&(x=w)}if(x)return x.endTime;let A=new Date;return A.setHours(0,0,0,0),A}let v=(0,J.default)(i,s.trim(),!0);if(v.isValid())return v.toDate();{ot.debug("Invalid date:"+i),ot.debug("With date format:"+s.trim());let x=new Date(i);if(x===void 0||isNaN(x.getTime())||x.getFullYear()<-1e4||x.getFullYear()>1e4)throw new Error("Invalid date:"+i);return x}},"getStartDate"),Ae=l(function(t){let s=/^(\d+(?:\.\d+)?)([Mdhmswy]|ms)$/.exec(t.trim());return s!==null?[Number.parseFloat(s[1]),s[2]]:[NaN,"ms"]},"parseDuration"),Fe=l(function(t,s,i,e=!1){i=i.trim();let g=/^until\s+(?<ids>[\d\w- ]+)/.exec(i);if(g!==null){let w=null;for(let W of g.groups.ids.split(" ")){let N=ut(W);N!==void 0&&(!w||N.startTime<w.startTime)&&(w=N)}if(w)return w.startTime;let R=new Date;return R.setHours(0,0,0,0),R}let v=(0,J.default)(i,s.trim(),!0);if(v.isValid())return e&&(v=v.add(1,"d")),v.toDate();let x=(0,J.default)(t),[A,F]=Ae(i);if(!Number.isNaN(A)){let w=x.add(A,F);w.isValid()&&(x=w)}return x.toDate()},"getEndDate"),Ct=0,mt=l(function(t){return t===void 0?(Ct=Ct+1,"task"+Ct):t},"parseId"),ws=l(function(t,s){let i;s.substr(0,1)===":"?i=s.substr(1,s.length):i=s;let e=i.split(","),a={};Yt(e,a,Ie);for(let v=0;v<e.length;v++)e[v]=e[v].trim();let g="";switch(e.length){case 1:a.id=mt(),a.startTime=t.endTime,g=e[0];break;case 2:a.id=mt(),a.startTime=Ht(void 0,tt,e[0]),g=e[1];break;case 3:a.id=mt(e[0]),a.startTime=Ht(void 0,tt,e[1]),g=e[2];break;default:}return g&&(a.endTime=Fe(a.startTime,tt,g,xt),a.manualEndTime=(0,J.default)(g,"YYYY-MM-DD",!0).isValid(),Le(a,tt,yt,kt)),a},"compileData"),_s=l(function(t,s){let i;s.substr(0,1)===":"?i=s.substr(1,s.length):i=s;let e=i.split(","),a={};Yt(e,a,Ie);for(let g=0;g<e.length;g++)e[g]=e[g].trim();switch(e.length){case 1:a.id=mt(),a.startTime={type:"prevTaskEnd",id:t},a.endTime={data:e[0]};break;case 2:a.id=mt(),a.startTime={type:"getStartDate",startData:e[0]},a.endTime={data:e[1]};break;case 3:a.id=mt(e[0]),a.startTime={type:"getStartDate",startData:e[1]},a.endTime={data:e[2]};break;default:}return a},"parseData"),Bt,Mt,X=[],Oe={},Ds=l(function(t,s){let i={section:gt,type:gt,processed:!1,manualEndTime:!1,renderEndTime:null,raw:{data:s},task:t,classes:[]},e=_s(Mt,s);i.raw.startTime=e.startTime,i.raw.endTime=e.endTime,i.id=e.id,i.prevTaskId=Mt,i.active=e.active,i.done=e.done,i.crit=e.crit,i.milestone=e.milestone,i.vert=e.vert,i.vert?i.order=-1:(i.order=jt,jt++);let a=X.push(i);Mt=i.id,Oe[i.id]=a-1},"addTask"),ut=l(function(t){let s=Oe[t];return X[s]},"findTaskById"),Ss=l(function(t,s){let i={section:gt,type:gt,description:t,task:t,classes:[]},e=ws(Bt,s);i.startTime=e.startTime,i.endTime=e.endTime,i.id=e.id,i.active=e.active,i.done=e.done,i.crit=e.crit,i.milestone=e.milestone,i.vert=e.vert,Bt=i,Et.push(i)},"addTaskOrg"),Me=l(function(){let t=l(function(i){let e=X[i],a="";switch(X[i].raw.startTime.type){case"prevTaskEnd":{let g=ut(e.prevTaskId);e.startTime=g.endTime;break}case"getStartDate":a=Ht(void 0,tt,X[i].raw.startTime.startData),a&&(X[i].startTime=a);break}return X[i].startTime&&(X[i].endTime=Fe(X[i].startTime,tt,X[i].raw.endTime.data,xt),X[i].endTime&&(X[i].processed=!0,X[i].manualEndTime=(0,J.default)(X[i].raw.endTime.data,"YYYY-MM-DD",!0).isValid(),Le(X[i],tt,yt,kt))),X[i].processed},"compileTask"),s=!0;for(let[i,e]of X.entries())t(i),s=s&&e.processed;return s},"compileTasks"),Cs=l(function(t,s){let i=s;lt().securityLevel!=="loose"&&(i=(0,je.sanitizeUrl)(s)),t.split(",").forEach(function(e){ut(e)!==void 0&&(Pe(e,()=>{window.open(i,"_self")}),Ut.set(e,i))}),We(t,"clickable")},"setLink"),We=l(function(t,s){t.split(",").forEach(function(i){let e=ut(i);e!==void 0&&e.classes.push(s)})},"setClass"),Ms=l(function(t,s,i){if(lt().securityLevel!=="loose"||s===void 0)return;let e=[];if(typeof i=="string"){e=i.split(/,(?=(?:(?:[^"]*"){2})*[^"]*$)/);for(let g=0;g<e.length;g++){let v=e[g].trim();v.startsWith('"')&&v.endsWith('"')&&(v=v.substr(1,v.length-2)),e[g]=v}}e.length===0&&e.push(t),ut(t)!==void 0&&Pe(t,()=>{Se.runFunc(s,...e)})},"setClickFun"),Pe=l(function(t,s){Jt.push(function(){let i=ht?`${ht}-${t}`:t,e=document.querySelector(`[id="${i}"]`);e!==null&&e.addEventListener("click",function(){s()})},function(){let i=ht?`${ht}-${t}`:t,e=document.querySelector(`[id="${i}-text"]`);e!==null&&e.addEventListener("click",function(){s()})})},"pushFun"),Es=l(function(t,s,i){t.split(",").forEach(function(e){Ms(e,s,i)}),We(t,"clickable")},"setClickEvent"),Is=l(function(t){Jt.forEach(function(s){s(t)})},"bindFunctions"),Ys={getConfig:l(()=>lt().gantt,"getConfig"),clear:Ue,setDateFormat:is,getDateFormat:us,enableInclusiveEndDates:rs,endDatesAreInclusive:ns,enableTopAxis:as,topAxisEnabled:os,setAxisFormat:Qe,getAxisFormat:Je,setTickInterval:Ke,getTickInterval:ts,setTodayMarker:es,getTodayMarker:ss,setAccTitle:ne,getAccTitle:ae,setDiagramTitle:le,getDiagramTitle:ue,setDiagramId:Ze,setDisplayMode:cs,getDisplayMode:ls,setAccDescription:oe,getAccDescription:ce,addSection:ys,getSections:gs,getTasks:ps,addTask:Ds,findTaskById:ut,addTaskOrg:Ss,setIncludes:ds,getIncludes:fs,setExcludes:hs,getExcludes:ms,setClickEvent:Es,setLink:Cs,getLinks:ks,bindFunctions:Is,parseDuration:Ae,isInvalidDate:$e,setWeekday:vs,getWeekday:Ts,setWeekend:bs};function Yt(t,s,i){let e=!0;for(;e;)e=!1,i.forEach(function(a){let g="^\\s*"+a+"\\s*$",v=new RegExp(g);t[0].match(v)&&(s[a]=!0,t.shift(1),e=!0)})}E(Yt,"getTaskTags");l(Yt,"getTaskTags");bt.default.extend(Ge.default);var $s=l(function(){ot.debug("Something is calling, setConf, remove the call")},"setConf"),Ee={monday:pe,tuesday:ve,wednesday:Te,thursday:be,friday:xe,saturday:we,sunday:ge},Ls=l((t,s)=>{let i=[...t].map(()=>-1/0),e=[...t].sort((g,v)=>g.startTime-v.startTime||g.order-v.order),a=0;for(let g of e)for(let v=0;v<i.length;v++)if(g.startTime>=i[v]){i[v]=g.endTime,g.order=v+s,v>a&&(a=v);break}return a},"getMaxIntersections"),it,zt=1e4,As=l(function(t,s,i,e){let a=lt().gantt;e.db.setDiagramId(s);let g=lt().securityLevel,v;g==="sandbox"&&(v=Tt("#i"+s));let x=g==="sandbox"?Tt(v.nodes()[0].contentDocument.body):Tt("body"),A=g==="sandbox"?v.nodes()[0].contentDocument:document,F=A.getElementById(s);it=F.parentElement.offsetWidth,it===void 0&&(it=1200),a.useWidth!==void 0&&(it=a.useWidth);let w=e.db.getTasks(),R=w.filter(m=>!m.vert),W=[];for(let m of R)W.push(m.type);W=I(W);let N={},U=2*a.topPadding;if(e.db.getDisplayMode()==="compact"||a.displayMode==="compact"){let m={};for(let p of R)m[p.section]===void 0?m[p.section]=[p]:m[p.section].push(p);let T=0;for(let p of Object.keys(m)){let y=Ls(m[p],T)+1;T+=y,U+=y*(a.barHeight+a.barGap),N[p]=y}}else{U+=R.length*(a.barHeight+a.barGap);for(let m of W)N[m]=R.filter(T=>T.type===m).length}F.setAttribute("viewBox","0 0 "+it+" "+U);let j=x.select(`[id="${s}"]`),k=_e().domain([fe(w,function(m){return m.startTime}),de(w,function(m){return m.endTime})]).rangeRound([0,it-a.leftPadding-a.rightPadding]);function S(m,T){let p=m.startTime,y=T.startTime,n=0;return p>y?n=1:p<y&&(n=-1),n}E(S,"taskCompare"),l(S,"taskCompare"),w.sort(S),P(w,it,U),ie(j,U,it,a.useMaxWidth),j.append("text").text(e.db.getDiagramTitle()).attr("x",it/2).attr("y",a.titleTopMargin).attr("class","titleText");function P(m,T,p){let y=a.barHeight,n=y+a.barGap,d=a.topPadding,f=a.leftPadding,u=ye().domain([0,W.length]).range(["#00B9FA","#F95002"]).interpolate(ke);G(n,d,f,T,p,m,e.db.getExcludes(),e.db.getIncludes()),Y(f,d,T,p),$(m,n,d,f,y,u,T,p),b(n,d,f,y,u),h(f,d,T,p)}E(P,"makeGantt"),l(P,"makeGantt");function $(m,T,p,y,n,d,f){m.sort((c,D)=>c.vert===D.vert?0:c.vert?1:-1);let u=m.filter(c=>!c.vert),r=[...new Set(u.map(c=>c.order))].map(c=>u.find(D=>D.order===c));j.append("g").selectAll("rect").data(r).enter().append("rect").attr("x",0).attr("y",function(c,D){return D=c.order,D*T+p-2}).attr("width",function(){return f-a.rightPadding/2}).attr("height",T).attr("class",function(c){for(let[D,M]of W.entries())if(c.type===M)return"section section"+D%a.numberSectionStyles;return"section section0"}).enter();let C=j.append("g").selectAll("rect").data(m).enter(),o=e.db.getLinks();if(C.append("rect").attr("id",function(c){return s+"-"+c.id}).attr("rx",3).attr("ry",3).attr("x",function(c){return c.milestone?k(c.startTime)+y+.5*(k(c.endTime)-k(c.startTime))-.5*n:k(c.startTime)+y}).attr("y",function(c,D){return D=c.order,c.vert?a.gridLineStartPadding:D*T+p}).attr("width",function(c){return c.milestone?n:c.vert?.08*n:k(c.renderEndTime||c.endTime)-k(c.startTime)}).attr("height",function(c){return c.vert?u.length*(a.barHeight+a.barGap)+a.barHeight*2:n}).attr("transform-origin",function(c,D){return D=c.order,(k(c.startTime)+y+.5*(k(c.endTime)-k(c.startTime))).toString()+"px "+(D*T+p+.5*n).toString()+"px"}).attr("class",function(c){let D="task",M="";c.classes.length>0&&(M=c.classes.join(" "));let V=0;for(let[L,O]of W.entries())c.type===O&&(V=L%a.numberSectionStyles);let z="";return c.active?c.crit?z+=" activeCrit":z=" active":c.done?c.crit?z=" doneCrit":z=" done":c.crit&&(z+=" crit"),z.length===0&&(z=" task"),c.milestone&&(z=" milestone "+z),c.vert&&(z=" vert "+z),z+=V,z+=" "+M,D+z}),C.append("text").attr("id",function(c){return s+"-"+c.id+"-text"}).text(function(c){return c.task}).attr("font-size",a.fontSize).attr("x",function(c){let D=k(c.startTime),M=k(c.renderEndTime||c.endTime);if(c.milestone&&(D+=.5*(k(c.endTime)-k(c.startTime))-.5*n,M=D+n),c.vert)return k(c.startTime)+y;let V=this.getBBox().width;return V>M-D?M+V+1.5*a.leftPadding>f?D+y-5:M+y+5:(M-D)/2+D+y}).attr("y",function(c,D){return c.vert?a.gridLineStartPadding+u.length*(a.barHeight+a.barGap)+60:(D=c.order,D*T+a.barHeight/2+(a.fontSize/2-2)+p)}).attr("text-height",n).attr("class",function(c){let D=k(c.startTime),M=k(c.endTime);c.milestone&&(M=D+n);let V=this.getBBox().width,z="";c.classes.length>0&&(z=c.classes.join(" "));let L=0;for(let[rt,et]of W.entries())c.type===et&&(L=rt%a.numberSectionStyles);let O="";return c.active&&(c.crit?O="activeCritText"+L:O="activeText"+L),c.done?c.crit?O=O+" doneCritText"+L:O=O+" doneText"+L:c.crit&&(O=O+" critText"+L),c.milestone&&(O+=" milestoneText"),c.vert&&(O+=" vertText"),V>M-D?M+V+1.5*a.leftPadding>f?z+" taskTextOutsideLeft taskTextOutside"+L+" "+O:z+" taskTextOutsideRight taskTextOutside"+L+" "+O+" width-"+V:z+" taskText taskText"+L+" "+O+" width-"+V}),lt().securityLevel==="sandbox"){let c;c=Tt("#i"+s);let D=c.nodes()[0].contentDocument;C.filter(function(M){return o.has(M.id)}).each(function(M){var V=D.querySelector("#"+CSS.escape(s+"-"+M.id)),z=D.querySelector("#"+CSS.escape(s+"-"+M.id+"-text"));let L=V.parentNode;var O=D.createElement("a");O.setAttribute("xlink:href",o.get(M.id)),O.setAttribute("target","_top"),L.appendChild(O),O.appendChild(V),O.appendChild(z)})}}E($,"drawRects"),l($,"drawRects");function G(m,T,p,y,n,d,f,u){if(f.length===0&&u.length===0)return;let _,r;for(let{startTime:M,endTime:V}of d)(_===void 0||M<_)&&(_=M),(r===void 0||V>r)&&(r=V);if(!_||!r)return;if((0,bt.default)(r).diff((0,bt.default)(_),"year")>5){ot.warn("The difference between the min and max time is more than 5 years. This will cause performance issues. Skipping drawing exclude days.");return}let C=e.db.getDateFormat(),o=[],H=null,c=(0,bt.default)(_);for(;c.valueOf()<=r;)e.db.isInvalidDate(c,C,f,u)?H?H.end=c:H={start:c,end:c}:H&&(o.push(H),H=null),c=c.add(1,"d");j.append("g").selectAll("rect").data(o).enter().append("rect").attr("id",M=>s+"-exclude-"+M.start.format("YYYY-MM-DD")).attr("x",M=>k(M.start.startOf("day"))+p).attr("y",a.gridLineStartPadding).attr("width",M=>k(M.end.endOf("day"))-k(M.start.startOf("day"))).attr("height",n-T-a.gridLineStartPadding).attr("transform-origin",function(M,V){return(k(M.start)+p+.5*(k(M.end)-k(M.start))).toString()+"px "+(V*m+.5*n).toString()+"px"}).attr("class","exclude-range")}E(G,"drawExcludeDays"),l(G,"drawExcludeDays");function B(m,T,p,y){if(p<=0||m>T)return 1/0;let n=T-m,d=bt.default.duration({[y??"day"]:p}).asMilliseconds();return d<=0?1/0:Math.ceil(n/d)}E(B,"getEstimatedTickCount"),l(B,"getEstimatedTickCount");function Y(m,T,p,y){let n=e.db.getDateFormat(),d=e.db.getAxisFormat(),f;d?f=d:n==="D"?f="%d":f=a.axisFormat??"%Y-%m-%d";let u=me(k).tickSize(-y+T+a.gridLineStartPadding).tickFormat(Rt(f)),r=/^([1-9]\d*)(millisecond|second|minute|hour|day|week|month)$/.exec(e.db.getTickInterval()||a.tickInterval);if(r!==null){let C=parseInt(r[1],10);if(isNaN(C)||C<=0)ot.warn(`Invalid tick interval value: "${r[1]}". Skipping custom tick interval.`);else{let o=r[2],H=e.db.getWeekday()||a.weekday,c=k.domain(),D=c[0],M=c[1],V=B(D,M,C,o);if(V>zt)ot.warn(`The tick interval "${C}${o}" would generate ${V} ticks, which exceeds the maximum allowed (${zt}). This may indicate an invalid date or time range. Skipping custom tick interval.`);else switch(o){case"millisecond":u.ticks(At.every(C));break;case"second":u.ticks(Ft.every(C));break;case"minute":u.ticks(Ot.every(C));break;case"hour":u.ticks(Wt.every(C));break;case"day":u.ticks(Pt.every(C));break;case"week":u.ticks(Ee[H].every(C));break;case"month":u.ticks(Vt.every(C));break}}}if(j.append("g").attr("class","grid").attr("transform","translate("+m+", "+(y-50)+")").call(u).selectAll("text").style("text-anchor","middle").attr("fill","#000").attr("stroke","none").attr("font-size",10).attr("dy","1em"),e.db.topAxisEnabled()||a.topAxis){let C=he(k).tickSize(-y+T+a.gridLineStartPadding).tickFormat(Rt(f));if(r!==null){let o=parseInt(r[1],10);if(isNaN(o)||o<=0)ot.warn(`Invalid tick interval value: "${r[1]}". Skipping custom tick interval.`);else{let H=r[2],c=e.db.getWeekday()||a.weekday,D=k.domain(),M=D[0],V=D[1];if(B(M,V,o,H)<=zt)switch(H){case"millisecond":C.ticks(At.every(o));break;case"second":C.ticks(Ft.every(o));break;case"minute":C.ticks(Ot.every(o));break;case"hour":C.ticks(Wt.every(o));break;case"day":C.ticks(Pt.every(o));break;case"week":C.ticks(Ee[c].every(o));break;case"month":C.ticks(Vt.every(o));break}}}j.append("g").attr("class","grid").attr("transform","translate("+m+", "+T+")").call(C).selectAll("text").style("text-anchor","middle").attr("fill","#000").attr("stroke","none").attr("font-size",10)}}E(Y,"makeGrid"),l(Y,"makeGrid");function b(m,T){let p=0,y=Object.keys(N).map(n=>[n,N[n]]);j.append("g").selectAll("text").data(y).enter().append(function(n){let d=n[0].split(se.lineBreakRegex),f=-(d.length-1)/2,u=A.createElementNS("http://www.w3.org/2000/svg","text");u.setAttribute("dy",f+"em");for(let[_,r]of d.entries()){let C=A.createElementNS("http://www.w3.org/2000/svg","tspan");C.setAttribute("alignment-baseline","central"),C.setAttribute("x","10"),_>0&&C.setAttribute("dy","1em"),C.textContent=r,u.appendChild(C)}return u}).attr("x",10).attr("y",function(n,d){if(d>0)for(let f=0;f<d;f++)return p+=y[d-1][1],n[1]*m/2+p*m+T;else return n[1]*m/2+T}).attr("font-size",a.sectionFontSize).attr("class",function(n){for(let[d,f]of W.entries())if(n[0]===f)return"sectionTitle sectionTitle"+d%a.numberSectionStyles;return"sectionTitle"})}E(b,"vertLabels"),l(b,"vertLabels");function h(m,T,p,y){let n=e.db.getTodayMarker();if(n==="off")return;let d=j.append("g").attr("class","today"),f=new Date,u=d.append("line");u.attr("x1",k(f)+m).attr("x2",k(f)+m).attr("y1",a.titleTopMargin).attr("y2",y-a.titleTopMargin).attr("class","today"),n!==""&&u.attr("style",n.replace(/,/g,";"))}E(h,"drawToday"),l(h,"drawToday");function I(m){let T={},p=[];for(let y=0,n=m.length;y<n;++y)Object.prototype.hasOwnProperty.call(T,m[y])||(T[m[y]]=!0,p.push(m[y]));return p}E(I,"checkUnique"),l(I,"checkUnique")},"draw"),Fs={setConf:$s,draw:As},Os=l(t=>`
  .mermaid-main-font {
        font-family: ${t.fontFamily};
  }

  .exclude-range {
    fill: ${t.excludeBkgColor};
  }

  .section {
    stroke: none;
    opacity: 0.2;
  }

  .section0 {
    fill: ${t.sectionBkgColor};
  }

  .section2 {
    fill: ${t.sectionBkgColor2};
  }

  .section1,
  .section3 {
    fill: ${t.altSectionBkgColor};
    opacity: 0.2;
  }

  .sectionTitle0 {
    fill: ${t.titleColor};
  }

  .sectionTitle1 {
    fill: ${t.titleColor};
  }

  .sectionTitle2 {
    fill: ${t.titleColor};
  }

  .sectionTitle3 {
    fill: ${t.titleColor};
  }

  .sectionTitle {
    text-anchor: start;
    font-family: ${t.fontFamily};
  }


  /* Grid and axis */

  .grid .tick {
    stroke: ${t.gridColor};
    opacity: 0.8;
    shape-rendering: crispEdges;
  }

  .grid .tick text {
    font-family: ${t.fontFamily};
    fill: ${t.textColor};
  }

  .grid path {
    stroke-width: 0;
  }


  /* Today line */

  .today {
    fill: none;
    stroke: ${t.todayLineColor};
    stroke-width: 2px;
  }


  /* Task styling */

  /* Default task */

  .task {
    stroke-width: 2;
  }

  .taskText {
    text-anchor: middle;
    font-family: ${t.fontFamily};
  }

  .taskTextOutsideRight {
    fill: ${t.taskTextDarkColor};
    text-anchor: start;
    font-family: ${t.fontFamily};
  }

  .taskTextOutsideLeft {
    fill: ${t.taskTextDarkColor};
    text-anchor: end;
  }


  /* Special case clickable */

  .task.clickable {
    cursor: pointer;
  }

  .taskText.clickable {
    cursor: pointer;
    fill: ${t.taskTextClickableColor} !important;
    font-weight: bold;
  }

  .taskTextOutsideLeft.clickable {
    cursor: pointer;
    fill: ${t.taskTextClickableColor} !important;
    font-weight: bold;
  }

  .taskTextOutsideRight.clickable {
    cursor: pointer;
    fill: ${t.taskTextClickableColor} !important;
    font-weight: bold;
  }


  /* Specific task settings for the sections*/

  .taskText0,
  .taskText1,
  .taskText2,
  .taskText3 {
    fill: ${t.taskTextColor};
  }

  .task0,
  .task1,
  .task2,
  .task3 {
    fill: ${t.taskBkgColor};
    stroke: ${t.taskBorderColor};
  }

  .taskTextOutside0,
  .taskTextOutside2
  {
    fill: ${t.taskTextOutsideColor};
  }

  .taskTextOutside1,
  .taskTextOutside3 {
    fill: ${t.taskTextOutsideColor};
  }


  /* Active task */

  .active0,
  .active1,
  .active2,
  .active3 {
    fill: ${t.activeTaskBkgColor};
    stroke: ${t.activeTaskBorderColor};
  }

  .activeText0,
  .activeText1,
  .activeText2,
  .activeText3 {
    fill: ${t.taskTextDarkColor} !important;
  }


  /* Completed task */

  .done0,
  .done1,
  .done2,
  .done3 {
    stroke: ${t.doneTaskBorderColor};
    fill: ${t.doneTaskBkgColor};
    stroke-width: 2;
  }

  .doneText0,
  .doneText1,
  .doneText2,
  .doneText3 {
    fill: ${t.taskTextDarkColor} !important;
  }

  /* Done task text displayed outside the bar sits against the diagram background,
     not against the done-task bar, so it must use the outside/contrast color. */
  .doneText0.taskTextOutsideLeft,
  .doneText0.taskTextOutsideRight,
  .doneText1.taskTextOutsideLeft,
  .doneText1.taskTextOutsideRight,
  .doneText2.taskTextOutsideLeft,
  .doneText2.taskTextOutsideRight,
  .doneText3.taskTextOutsideLeft,
  .doneText3.taskTextOutsideRight {
    fill: ${t.taskTextOutsideColor} !important;
  }


  /* Tasks on the critical line */

  .crit0,
  .crit1,
  .crit2,
  .crit3 {
    stroke: ${t.critBorderColor};
    fill: ${t.critBkgColor};
    stroke-width: 2;
  }

  .activeCrit0,
  .activeCrit1,
  .activeCrit2,
  .activeCrit3 {
    stroke: ${t.critBorderColor};
    fill: ${t.activeTaskBkgColor};
    stroke-width: 2;
  }

  .doneCrit0,
  .doneCrit1,
  .doneCrit2,
  .doneCrit3 {
    stroke: ${t.critBorderColor};
    fill: ${t.doneTaskBkgColor};
    stroke-width: 2;
    cursor: pointer;
    shape-rendering: crispEdges;
  }

  .milestone {
    transform: rotate(45deg) scale(0.8,0.8);
  }

  .milestoneText {
    font-style: italic;
  }
  .doneCritText0,
  .doneCritText1,
  .doneCritText2,
  .doneCritText3 {
    fill: ${t.taskTextDarkColor} !important;
  }

  /* Done-crit task text outside the bar \u2014 same reasoning as doneText above. */
  .doneCritText0.taskTextOutsideLeft,
  .doneCritText0.taskTextOutsideRight,
  .doneCritText1.taskTextOutsideLeft,
  .doneCritText1.taskTextOutsideRight,
  .doneCritText2.taskTextOutsideLeft,
  .doneCritText2.taskTextOutsideRight,
  .doneCritText3.taskTextOutsideLeft,
  .doneCritText3.taskTextOutsideRight {
    fill: ${t.taskTextOutsideColor} !important;
  }

  .vert {
    stroke: ${t.vertLineColor};
  }

  .vertText {
    font-size: 15px;
    text-anchor: middle;
    fill: ${t.vertLineColor} !important;
  }

  .activeCritText0,
  .activeCritText1,
  .activeCritText2,
  .activeCritText3 {
    fill: ${t.taskTextDarkColor} !important;
  }

  .titleText {
    text-anchor: middle;
    font-size: 18px;
    fill: ${t.titleColor||t.textColor};
    font-family: ${t.fontFamily};
  }
`,"getStyles"),Ws=Os,Bs={parser:Xe,db:Ys,renderer:Fs,styles:Ws};export{Bs as diagram};
