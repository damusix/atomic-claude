import{a as Kt}from"./chunk-VON2Y57W.js";import{b as Ht}from"./chunk-RH6BRR6A.js";import{a as Xt}from"./chunk-ZQIFZDQX.js";import{h as zt}from"./chunk-YCKBSAQO.js";import{g as Wt,p as jt}from"./chunk-RANDTXFT.js";import{N as U,T as Pt,U as Bt,V as Yt,W as Gt,X as Vt,Y as Mt,Z as Ut,_ as $,g as Ft}from"./chunk-BAGAVWDO.js";import{b as E,h as yt}from"./chunk-VYE4DNDP.js";import{a as f}from"./chunk-NKM6JPMV.js";import{d as m}from"./chunk-RPKWB5KU.js";var xt=(function(){var t=f(function(M,a,u,i){for(u=u||{},i=M.length;i--;u[M[i]]=a);return u},"o"),e=[1,2],l=[1,3],s=[1,4],c=[2,4],h=[1,9],p=[1,11],y=[1,16],o=[1,17],b=[1,18],k=[1,19],O=[1,33],L=[1,20],D=[1,21],d=[1,22],I=[1,23],F=[1,24],A=[1,26],P=[1,27],x=[1,28],B=[1,29],w=[1,30],z=[1,31],nt=[1,32],ot=[1,35],lt=[1,36],ct=[1,37],ht=[1,38],K=[1,34],S=[1,4,5,16,17,19,21,22,24,25,26,27,28,29,33,35,37,38,41,45,48,51,52,53,54,57],ut=[1,4,5,14,15,16,17,19,21,22,24,25,26,27,28,29,33,35,37,38,39,40,41,45,48,51,52,53,54,57],Ot=[4,5,16,17,19,21,22,24,25,26,27,28,29,33,35,37,38,41,45,48,51,52,53,54,57],Tt={trace:f(m(function(){},"trace"),"trace"),yy:{},symbols_:{error:2,start:3,SPACE:4,NL:5,SD:6,document:7,line:8,statement:9,classDefStatement:10,styleStatement:11,cssClassStatement:12,idStatement:13,DESCR:14,"-->":15,HIDE_EMPTY:16,scale:17,WIDTH:18,COMPOSIT_STATE:19,STRUCT_START:20,STRUCT_STOP:21,STATE_DESCR:22,AS:23,ID:24,FORK:25,JOIN:26,CHOICE:27,CONCURRENT:28,note:29,notePosition:30,NOTE_TEXT:31,direction:32,acc_title:33,acc_title_value:34,acc_descr:35,acc_descr_value:36,acc_descr_multiline_value:37,CLICK:38,STRING:39,HREF:40,classDef:41,CLASSDEF_ID:42,CLASSDEF_STYLEOPTS:43,DEFAULT:44,style:45,STYLE_IDS:46,STYLEDEF_STYLEOPTS:47,class:48,CLASSENTITY_IDS:49,STYLECLASS:50,direction_tb:51,direction_bt:52,direction_rl:53,direction_lr:54,eol:55,";":56,EDGE_STATE:57,STYLE_SEPARATOR:58,left_of:59,right_of:60,$accept:0,$end:1},terminals_:{2:"error",4:"SPACE",5:"NL",6:"SD",14:"DESCR",15:"-->",16:"HIDE_EMPTY",17:"scale",18:"WIDTH",19:"COMPOSIT_STATE",20:"STRUCT_START",21:"STRUCT_STOP",22:"STATE_DESCR",23:"AS",24:"ID",25:"FORK",26:"JOIN",27:"CHOICE",28:"CONCURRENT",29:"note",31:"NOTE_TEXT",33:"acc_title",34:"acc_title_value",35:"acc_descr",36:"acc_descr_value",37:"acc_descr_multiline_value",38:"CLICK",39:"STRING",40:"HREF",41:"classDef",42:"CLASSDEF_ID",43:"CLASSDEF_STYLEOPTS",44:"DEFAULT",45:"style",46:"STYLE_IDS",47:"STYLEDEF_STYLEOPTS",48:"class",49:"CLASSENTITY_IDS",50:"STYLECLASS",51:"direction_tb",52:"direction_bt",53:"direction_rl",54:"direction_lr",56:";",57:"EDGE_STATE",58:"STYLE_SEPARATOR",59:"left_of",60:"right_of"},productions_:[0,[3,2],[3,2],[3,2],[7,0],[7,2],[8,2],[8,1],[8,1],[9,1],[9,1],[9,1],[9,1],[9,2],[9,3],[9,4],[9,1],[9,2],[9,1],[9,4],[9,3],[9,6],[9,1],[9,1],[9,1],[9,1],[9,4],[9,4],[9,1],[9,2],[9,2],[9,1],[9,5],[9,5],[10,3],[10,3],[11,3],[12,3],[32,1],[32,1],[32,1],[32,1],[55,1],[55,1],[13,1],[13,1],[13,3],[13,3],[30,1],[30,1]],performAction:f(m(function(a,u,i,g,T,r,X){var n=r.length-1;switch(T){case 3:return g.setRootDoc(r[n]),r[n];break;case 4:this.$=[];break;case 5:r[n]!="nl"&&(r[n-1].push(r[n]),this.$=r[n-1]);break;case 6:case 7:this.$=r[n];break;case 8:this.$="nl";break;case 12:this.$=r[n];break;case 13:let dt=r[n-1];dt.description=g.trimColon(r[n]),this.$=dt;break;case 14:this.$={stmt:"relation",state1:r[n-2],state2:r[n]};break;case 15:let ft=g.trimColon(r[n]);this.$={stmt:"relation",state1:r[n-3],state2:r[n-1],description:ft};break;case 19:this.$={stmt:"state",id:r[n-3],type:"default",description:"",doc:r[n-1]};break;case 20:var Y=r[n],G=r[n-2].trim();if(r[n].match(":")){var et=r[n].split(":");Y=et[0],G=[G,et[1]]}this.$={stmt:"state",id:Y,type:"default",description:G};break;case 21:this.$={stmt:"state",id:r[n-3],type:"default",description:r[n-5],doc:r[n-1]};break;case 22:this.$={stmt:"state",id:r[n],type:"fork"};break;case 23:this.$={stmt:"state",id:r[n],type:"join"};break;case 24:this.$={stmt:"state",id:r[n],type:"choice"};break;case 25:this.$={stmt:"state",id:g.getDividerId(),type:"divider"};break;case 26:this.$={stmt:"state",id:r[n-1].trim(),note:{position:r[n-2].trim(),text:r[n].trim()}};break;case 29:this.$=r[n].trim(),g.setAccTitle(this.$);break;case 30:case 31:this.$=r[n].trim(),g.setAccDescription(this.$);break;case 32:this.$={stmt:"click",id:r[n-3],url:r[n-2],tooltip:r[n-1]};break;case 33:this.$={stmt:"click",id:r[n-3],url:r[n-1],tooltip:""};break;case 34:case 35:this.$={stmt:"classDef",id:r[n-1].trim(),classes:r[n].trim()};break;case 36:this.$={stmt:"style",id:r[n-1].trim(),styleClass:r[n].trim()};break;case 37:this.$={stmt:"applyClass",id:r[n-1].trim(),styleClass:r[n].trim()};break;case 38:g.setDirection("TB"),this.$={stmt:"dir",value:"TB"};break;case 39:g.setDirection("BT"),this.$={stmt:"dir",value:"BT"};break;case 40:g.setDirection("RL"),this.$={stmt:"dir",value:"RL"};break;case 41:g.setDirection("LR"),this.$={stmt:"dir",value:"LR"};break;case 44:case 45:this.$={stmt:"state",id:r[n].trim(),type:"default",description:""};break;case 46:this.$={stmt:"state",id:r[n-2].trim(),classes:[r[n].trim()],type:"default",description:""};break;case 47:this.$={stmt:"state",id:r[n-2].trim(),classes:[r[n].trim()],type:"default",description:""};break}},"anonymous"),"anonymous"),table:[{3:1,4:e,5:l,6:s},{1:[3]},{3:5,4:e,5:l,6:s},{3:6,4:e,5:l,6:s},t([1,4,5,16,17,19,22,24,25,26,27,28,29,33,35,37,38,41,45,48,51,52,53,54,57],c,{7:7}),{1:[2,1]},{1:[2,2]},{1:[2,3],4:h,5:p,8:8,9:10,10:12,11:13,12:14,13:15,16:y,17:o,19:b,22:k,24:O,25:L,26:D,27:d,28:I,29:F,32:25,33:A,35:P,37:x,38:B,41:w,45:z,48:nt,51:ot,52:lt,53:ct,54:ht,57:K},t(S,[2,5]),{9:39,10:12,11:13,12:14,13:15,16:y,17:o,19:b,22:k,24:O,25:L,26:D,27:d,28:I,29:F,32:25,33:A,35:P,37:x,38:B,41:w,45:z,48:nt,51:ot,52:lt,53:ct,54:ht,57:K},t(S,[2,7]),t(S,[2,8]),t(S,[2,9]),t(S,[2,10]),t(S,[2,11]),t(S,[2,12],{14:[1,40],15:[1,41]}),t(S,[2,16]),{18:[1,42]},t(S,[2,18],{20:[1,43]}),{23:[1,44]},t(S,[2,22]),t(S,[2,23]),t(S,[2,24]),t(S,[2,25]),{30:45,31:[1,46],59:[1,47],60:[1,48]},t(S,[2,28]),{34:[1,49]},{36:[1,50]},t(S,[2,31]),{13:51,24:O,57:K},{42:[1,52],44:[1,53]},{46:[1,54]},{49:[1,55]},t(ut,[2,44],{58:[1,56]}),t(ut,[2,45],{58:[1,57]}),t(S,[2,38]),t(S,[2,39]),t(S,[2,40]),t(S,[2,41]),t(S,[2,6]),t(S,[2,13]),{13:58,24:O,57:K},t(S,[2,17]),t(Ot,c,{7:59}),{24:[1,60]},{24:[1,61]},{23:[1,62]},{24:[2,48]},{24:[2,49]},t(S,[2,29]),t(S,[2,30]),{39:[1,63],40:[1,64]},{43:[1,65]},{43:[1,66]},{47:[1,67]},{50:[1,68]},{24:[1,69]},{24:[1,70]},t(S,[2,14],{14:[1,71]}),{4:h,5:p,8:8,9:10,10:12,11:13,12:14,13:15,16:y,17:o,19:b,21:[1,72],22:k,24:O,25:L,26:D,27:d,28:I,29:F,32:25,33:A,35:P,37:x,38:B,41:w,45:z,48:nt,51:ot,52:lt,53:ct,54:ht,57:K},t(S,[2,20],{20:[1,73]}),{31:[1,74]},{24:[1,75]},{39:[1,76]},{39:[1,77]},t(S,[2,34]),t(S,[2,35]),t(S,[2,36]),t(S,[2,37]),t(ut,[2,46]),t(ut,[2,47]),t(S,[2,15]),t(S,[2,19]),t(Ot,c,{7:78}),t(S,[2,26]),t(S,[2,27]),{5:[1,79]},{5:[1,80]},{4:h,5:p,8:8,9:10,10:12,11:13,12:14,13:15,16:y,17:o,19:b,21:[1,81],22:k,24:O,25:L,26:D,27:d,28:I,29:F,32:25,33:A,35:P,37:x,38:B,41:w,45:z,48:nt,51:ot,52:lt,53:ct,54:ht,57:K},t(S,[2,32]),t(S,[2,33]),t(S,[2,21])],defaultActions:{5:[2,1],6:[2,2],47:[2,48],48:[2,49]},parseError:f(m(function(a,u){if(u.recoverable)this.trace(a);else{var i=new Error(a);throw i.hash=u,i}},"parseError"),"parseError"),parse:f(m(function(a){var u=this,i=[0],g=[],T=[null],r=[],X=this.table,n="",Y=0,G=0,et=0,dt=2,ft=1,Se=r.slice.call(arguments,1),_=Object.create(this.lexer),j={yy:{}};for(var mt in this.yy)Object.prototype.hasOwnProperty.call(this.yy,mt)&&(j.yy[mt]=this.yy[mt]);_.setInput(a,j.yy),j.yy.lexer=_,j.yy.parser=this,typeof _.yylloc>"u"&&(_.yylloc={});var Et=_.yylloc;r.push(Et);var ye=_.options&&_.options.ranges;typeof j.yy.parseError=="function"?this.parseError=j.yy.parseError:this.parseError=Object.getPrototypeOf(this).parseError;function Rt(N){i.length=i.length-2*N,T.length=T.length-N,r.length=r.length-N}m(Rt,"popStack"),f(Rt,"popStack");function kt(){var N;return N=g.pop()||_.lex()||ft,typeof N!="number"&&(N instanceof Array&&(g=N,N=g.pop()),N=u.symbols_[N]||N),N}m(kt,"lex"),f(kt,"lex");for(var v,_t,H,R,Me,Dt,J={},pt,V,$t,St;;){if(H=i[i.length-1],this.defaultActions[H]?R=this.defaultActions[H]:((v===null||typeof v>"u")&&(v=kt()),R=X[H]&&X[H][v]),typeof R>"u"||!R.length||!R[0]){var vt="";St=[];for(pt in X[H])this.terminals_[pt]&&pt>dt&&St.push("'"+this.terminals_[pt]+"'");_.showPosition?vt="Parse error on line "+(Y+1)+`:
`+_.showPosition()+`
Expecting `+St.join(", ")+", got '"+(this.terminals_[v]||v)+"'":vt="Parse error on line "+(Y+1)+": Unexpected "+(v==ft?"end of input":"'"+(this.terminals_[v]||v)+"'"),this.parseError(vt,{text:_.match,token:this.terminals_[v]||v,line:_.yylineno,loc:Et,expected:St})}if(R[0]instanceof Array&&R.length>1)throw new Error("Parse Error: multiple actions possible at state: "+H+", token: "+v);switch(R[0]){case 1:i.push(v),T.push(_.yytext),r.push(_.yylloc),i.push(R[1]),v=null,_t?(v=_t,_t=null):(G=_.yyleng,n=_.yytext,Y=_.yylineno,Et=_.yylloc,et>0&&et--);break;case 2:if(V=this.productions_[R[1]][1],J.$=T[T.length-V],J._$={first_line:r[r.length-(V||1)].first_line,last_line:r[r.length-1].last_line,first_column:r[r.length-(V||1)].first_column,last_column:r[r.length-1].last_column},ye&&(J._$.range=[r[r.length-(V||1)].range[0],r[r.length-1].range[1]]),Dt=this.performAction.apply(J,[n,G,Y,j.yy,R[1],T,r].concat(Se)),typeof Dt<"u")return Dt;V&&(i=i.slice(0,-1*V*2),T=T.slice(0,-1*V),r=r.slice(0,-1*V)),i.push(this.productions_[R[1]][0]),T.push(J.$),r.push(J._$),$t=X[i[i.length-2]][i[i.length-1]],i.push($t);break;case 3:return!0}}return!0},"parse"),"parse")},pe=(function(){var M={EOF:1,parseError:f(m(function(u,i){if(this.yy.parser)this.yy.parser.parseError(u,i);else throw new Error(u)},"parseError"),"parseError"),setInput:f(function(a,u){return this.yy=u||this.yy||{},this._input=a,this._more=this._backtrack=this.done=!1,this.yylineno=this.yyleng=0,this.yytext=this.matched=this.match="",this.conditionStack=["INITIAL"],this.yylloc={first_line:1,first_column:0,last_line:1,last_column:0},this.options.ranges&&(this.yylloc.range=[0,0]),this.offset=0,this},"setInput"),input:f(function(){var a=this._input[0];this.yytext+=a,this.yyleng++,this.offset++,this.match+=a,this.matched+=a;var u=a.match(/(?:\r\n?|\n).*/g);return u?(this.yylineno++,this.yylloc.last_line++):this.yylloc.last_column++,this.options.ranges&&this.yylloc.range[1]++,this._input=this._input.slice(1),a},"input"),unput:f(function(a){var u=a.length,i=a.split(/(?:\r\n?|\n)/g);this._input=a+this._input,this.yytext=this.yytext.substr(0,this.yytext.length-u),this.offset-=u;var g=this.match.split(/(?:\r\n?|\n)/g);this.match=this.match.substr(0,this.match.length-1),this.matched=this.matched.substr(0,this.matched.length-1),i.length-1&&(this.yylineno-=i.length-1);var T=this.yylloc.range;return this.yylloc={first_line:this.yylloc.first_line,last_line:this.yylineno+1,first_column:this.yylloc.first_column,last_column:i?(i.length===g.length?this.yylloc.first_column:0)+g[g.length-i.length].length-i[0].length:this.yylloc.first_column-u},this.options.ranges&&(this.yylloc.range=[T[0],T[0]+this.yyleng-u]),this.yyleng=this.yytext.length,this},"unput"),more:f(function(){return this._more=!0,this},"more"),reject:f(function(){if(this.options.backtrack_lexer)this._backtrack=!0;else return this.parseError("Lexical error on line "+(this.yylineno+1)+`. You can only invoke reject() in the lexer when the lexer is of the backtracking persuasion (options.backtrack_lexer = true).
`+this.showPosition(),{text:"",token:null,line:this.yylineno});return this},"reject"),less:f(function(a){this.unput(this.match.slice(a))},"less"),pastInput:f(function(){var a=this.matched.substr(0,this.matched.length-this.match.length);return(a.length>20?"...":"")+a.substr(-20).replace(/\n/g,"")},"pastInput"),upcomingInput:f(function(){var a=this.match;return a.length<20&&(a+=this._input.substr(0,20-a.length)),(a.substr(0,20)+(a.length>20?"...":"")).replace(/\n/g,"")},"upcomingInput"),showPosition:f(function(){var a=this.pastInput(),u=new Array(a.length+1).join("-");return a+this.upcomingInput()+`
`+u+"^"},"showPosition"),test_match:f(function(a,u){var i,g,T;if(this.options.backtrack_lexer&&(T={yylineno:this.yylineno,yylloc:{first_line:this.yylloc.first_line,last_line:this.last_line,first_column:this.yylloc.first_column,last_column:this.yylloc.last_column},yytext:this.yytext,match:this.match,matches:this.matches,matched:this.matched,yyleng:this.yyleng,offset:this.offset,_more:this._more,_input:this._input,yy:this.yy,conditionStack:this.conditionStack.slice(0),done:this.done},this.options.ranges&&(T.yylloc.range=this.yylloc.range.slice(0))),g=a[0].match(/(?:\r\n?|\n).*/g),g&&(this.yylineno+=g.length),this.yylloc={first_line:this.yylloc.last_line,last_line:this.yylineno+1,first_column:this.yylloc.last_column,last_column:g?g[g.length-1].length-g[g.length-1].match(/\r?\n?/)[0].length:this.yylloc.last_column+a[0].length},this.yytext+=a[0],this.match+=a[0],this.matches=a,this.yyleng=this.yytext.length,this.options.ranges&&(this.yylloc.range=[this.offset,this.offset+=this.yyleng]),this._more=!1,this._backtrack=!1,this._input=this._input.slice(a[0].length),this.matched+=a[0],i=this.performAction.call(this,this.yy,this,u,this.conditionStack[this.conditionStack.length-1]),this.done&&this._input&&(this.done=!1),i)return i;if(this._backtrack){for(var r in T)this[r]=T[r];return!1}return!1},"test_match"),next:f(function(){if(this.done)return this.EOF;this._input||(this.done=!0);var a,u,i,g;this._more||(this.yytext="",this.match="");for(var T=this._currentRules(),r=0;r<T.length;r++)if(i=this._input.match(this.rules[T[r]]),i&&(!u||i[0].length>u[0].length)){if(u=i,g=r,this.options.backtrack_lexer){if(a=this.test_match(i,T[r]),a!==!1)return a;if(this._backtrack){u=!1;continue}else return!1}else if(!this.options.flex)break}return u?(a=this.test_match(u,T[g]),a!==!1?a:!1):this._input===""?this.EOF:this.parseError("Lexical error on line "+(this.yylineno+1)+`. Unrecognized text.
`+this.showPosition(),{text:"",token:null,line:this.yylineno})},"next"),lex:f(m(function(){var u=this.next();return u||this.lex()},"lex"),"lex"),begin:f(m(function(u){this.conditionStack.push(u)},"begin"),"begin"),popState:f(m(function(){var u=this.conditionStack.length-1;return u>0?this.conditionStack.pop():this.conditionStack[0]},"popState"),"popState"),_currentRules:f(m(function(){return this.conditionStack.length&&this.conditionStack[this.conditionStack.length-1]?this.conditions[this.conditionStack[this.conditionStack.length-1]].rules:this.conditions.INITIAL.rules},"_currentRules"),"_currentRules"),topState:f(m(function(u){return u=this.conditionStack.length-1-Math.abs(u||0),u>=0?this.conditionStack[u]:"INITIAL"},"topState"),"topState"),pushState:f(m(function(u){this.begin(u)},"pushState"),"pushState"),stateStackSize:f(m(function(){return this.conditionStack.length},"stateStackSize"),"stateStackSize"),options:{"case-insensitive":!0},performAction:f(m(function(u,i,g,T){function r(){let n=i.yytext.indexOf("%%");if(n===0)return!1;if(n>0){let Y=i.yytext.slice(0,n),G=i.yytext.slice(n);G&&u.lexer.unput(G),i.yytext=Y}return!0}m(r,"processId"),f(r,"processId");var X=T;switch(g){case 0:return 38;case 1:return 40;case 2:return 39;case 3:return 44;case 4:return 51;case 5:return 52;case 6:return 53;case 7:return 54;case 8:return 5;case 9:break;case 10:break;case 11:break;case 12:break;case 13:return this.pushState("SCALE"),17;break;case 14:return 18;case 15:this.popState();break;case 16:return this.begin("acc_title"),33;break;case 17:return this.popState(),"acc_title_value";break;case 18:return this.begin("acc_descr"),35;break;case 19:return this.popState(),"acc_descr_value";break;case 20:this.begin("acc_descr_multiline");break;case 21:this.popState();break;case 22:return"acc_descr_multiline_value";case 23:return this.pushState("CLASSDEF"),41;break;case 24:return this.popState(),this.pushState("CLASSDEFID"),"DEFAULT_CLASSDEF_ID";break;case 25:return this.popState(),this.pushState("CLASSDEFID"),42;break;case 26:return this.popState(),43;break;case 27:return this.pushState("CLASS"),48;break;case 28:return this.popState(),this.pushState("CLASS_STYLE"),49;break;case 29:return this.popState(),50;break;case 30:return this.pushState("STYLE"),45;break;case 31:return this.popState(),this.pushState("STYLEDEF_STYLES"),46;break;case 32:return this.popState(),47;break;case 33:return this.pushState("SCALE"),17;break;case 34:return 18;case 35:this.popState();break;case 36:this.pushState("STATE");break;case 37:return this.popState(),i.yytext=i.yytext.slice(0,-8).trim(),25;break;case 38:return this.popState(),i.yytext=i.yytext.slice(0,-8).trim(),26;break;case 39:return this.popState(),i.yytext=i.yytext.slice(0,-10).trim(),27;break;case 40:return this.popState(),i.yytext=i.yytext.slice(0,-8).trim(),25;break;case 41:return this.popState(),i.yytext=i.yytext.slice(0,-8).trim(),26;break;case 42:return this.popState(),i.yytext=i.yytext.slice(0,-10).trim(),27;break;case 43:return 51;case 44:return 52;case 45:return 53;case 46:return 54;case 47:this.pushState("STATE_STRING");break;case 48:return this.pushState("STATE_ID"),"AS";break;case 49:if(!r())return;return this.popState(),"ID";break;case 50:this.popState();break;case 51:return"STATE_DESCR";case 52:throw new Error('Error: State name must be a single word. Found: "'+i.yytext.trim()+'"');case 53:return 19;case 54:this.popState();break;case 55:return this.popState(),this.pushState("struct"),20;break;case 56:return this.popState(),21;break;case 57:break;case 58:return this.begin("NOTE"),29;break;case 59:return this.popState(),this.pushState("NOTE_ID"),59;break;case 60:return this.popState(),this.pushState("NOTE_ID"),60;break;case 61:this.popState(),this.pushState("FLOATING_NOTE");break;case 62:return this.popState(),this.pushState("FLOATING_NOTE_ID"),"AS";break;case 63:break;case 64:return"NOTE_TEXT";case 65:if(!r())return;return this.popState(),"ID";break;case 66:if(!r())return;return this.popState(),this.pushState("NOTE_TEXT"),24;break;case 67:return this.popState(),i.yytext=i.yytext.substr(2).trim(),31;break;case 68:return this.popState(),i.yytext=i.yytext.slice(0,-8).trim(),31;break;case 69:return 6;case 70:return 6;case 71:return 16;case 72:return 57;case 73:return r()?24:void 0;case 74:return i.yytext=i.yytext.trim(),14;break;case 75:return 15;case 76:return 28;case 77:return 58;case 78:return 5;case 79:return"INVALID"}},"anonymous"),"anonymous"),rules:[/^(?:click\b)/i,/^(?:href\b)/i,/^(?:"[^"]*")/i,/^(?:default\b)/i,/^(?:.*direction\s+TB[^\n]*)/i,/^(?:.*direction\s+BT[^\n]*)/i,/^(?:.*direction\s+RL[^\n]*)/i,/^(?:.*direction\s+LR[^\n]*)/i,/^(?:[\n]+)/i,/^(?:[\s]+)/i,/^(?:((?!\n)\s)+)/i,/^(?:#[^\n]*)/i,/^(?:%%(?!\{)[^\n]*)/i,/^(?:scale\s+)/i,/^(?:\d+)/i,/^(?:\s+width\b)/i,/^(?:accTitle\s*:\s*)/i,/^(?:(?!\n||)*[^\n]*)/i,/^(?:accDescr\s*:\s*)/i,/^(?:(?!\n||)*[^\n]*)/i,/^(?:accDescr\s*\{\s*)/i,/^(?:[\}])/i,/^(?:[^\}]*)/i,/^(?:classDef\s+)/i,/^(?:DEFAULT\s+)/i,/^(?:\w+\s+)/i,/^(?:[^\n]*)/i,/^(?:class\s+)/i,/^(?:(\w+)+((,\s*\w+)*))/i,/^(?:[^\n]*)/i,/^(?:style\s+)/i,/^(?:[\w,]+\s+)/i,/^(?:[^\n]*)/i,/^(?:scale\s+)/i,/^(?:\d+)/i,/^(?:\s+width\b)/i,/^(?:state\s+)/i,/^(?:.*<<fork>>)/i,/^(?:.*<<join>>)/i,/^(?:.*<<choice>>)/i,/^(?:.*\[\[fork\]\])/i,/^(?:.*\[\[join\]\])/i,/^(?:.*\[\[choice\]\])/i,/^(?:.*direction\s+TB[^\n]*)/i,/^(?:.*direction\s+BT[^\n]*)/i,/^(?:.*direction\s+RL[^\n]*)/i,/^(?:.*direction\s+LR[^\n]*)/i,/^(?:["])/i,/^(?:\s*as\s+)/i,/^(?:[^\n\{]*)/i,/^(?:["])/i,/^(?:[^"]*)/i,/^(?:\w+\s+\w+.*?\{)/i,/^(?:[^\n\s\{]+)/i,/^(?:\n)/i,/^(?:\{)/i,/^(?:\})/i,/^(?:[\n])/i,/^(?:note\s+)/i,/^(?:left of\b)/i,/^(?:right of\b)/i,/^(?:")/i,/^(?:\s*as\s*)/i,/^(?:["])/i,/^(?:[^"]*)/i,/^(?:[^\n]*)/i,/^(?:\s*[^:\n\s\-]+)/i,/^(?:\s*:[^:\n;]+)/i,/^(?:[\s\S]*?\n\s*end note\b)/i,/^(?:stateDiagram\s+)/i,/^(?:stateDiagram-v2\s+)/i,/^(?:hide empty description\b)/i,/^(?:\[\*\])/i,/^(?:[^:\n\s\-\{]+)/i,/^(?:\s*:(?:[^:\n;]|:[^:\n;])+)/i,/^(?:-->)/i,/^(?:--)/i,/^(?::::)/i,/^(?:$)/i,/^(?:.)/i],conditions:{LINE:{rules:[10,11,12],inclusive:!1},struct:{rules:[10,11,12,23,27,30,36,43,44,45,46,56,57,58,72,73,74,75,76,77],inclusive:!1},FLOATING_NOTE_ID:{rules:[65],inclusive:!1},FLOATING_NOTE:{rules:[62,63,64],inclusive:!1},NOTE_TEXT:{rules:[67,68],inclusive:!1},NOTE_ID:{rules:[66],inclusive:!1},NOTE:{rules:[59,60,61],inclusive:!1},STYLEDEF_STYLEOPTS:{rules:[],inclusive:!1},STYLEDEF_STYLES:{rules:[32],inclusive:!1},STYLE_IDS:{rules:[],inclusive:!1},STYLE:{rules:[31],inclusive:!1},CLASS_STYLE:{rules:[29],inclusive:!1},CLASS:{rules:[28],inclusive:!1},CLASSDEFID:{rules:[26],inclusive:!1},CLASSDEF:{rules:[24,25],inclusive:!1},acc_descr_multiline:{rules:[21,22],inclusive:!1},acc_descr:{rules:[19],inclusive:!1},acc_title:{rules:[17],inclusive:!1},SCALE:{rules:[14,15,34,35],inclusive:!1},ALIAS:{rules:[],inclusive:!1},STATE_ID:{rules:[49],inclusive:!1},STATE_STRING:{rules:[50,51],inclusive:!1},FORK_STATE:{rules:[],inclusive:!1},STATE:{rules:[10,11,12,37,38,39,40,41,42,47,48,52,53,54,55],inclusive:!1},ID:{rules:[10,11,12],inclusive:!1},INITIAL:{rules:[0,1,2,3,4,5,6,7,8,9,11,12,13,16,18,20,23,27,30,33,36,55,58,69,70,71,72,73,74,75,77,78,79],inclusive:!0}}};return M})();Tt.lexer=pe;function tt(){this.yy={}}return m(tt,"Parser"),f(tt,"Parser"),tt.prototype=Tt,Tt.Parser=tt,new tt})();xt.parser=xt;var Ze=xt,ge="TB",re="TB",Jt="dir",Q="state",q="root",Lt="relation",be="classDef",Te="style",me="applyClass",rt="default",ie="divider",ae="fill:none",ne="fill: #333",oe="c",le="markdown",ce="normal",Ct="rect",At="rectWithTitle",Ee="stateStart",ke="stateEnd",qt="divider",Qt="roundedWithTitle",_e="note",De="noteGroup",at="statediagram",ve="state",Ce=`${at}-${ve}`,he="transition",Ae="note",xe="note-edge",Le=`${he} ${xe}`,Ie=`${at}-${Ae}`,we="cluster",Ne=`${at}-${we}`,Oe="cluster-alt",Re=`${at}-${Oe}`,ue="parent",de="note",$e="state",It="----",Fe=`${It}${de}`,Zt=`${It}${ue}`,fe=f((t,e=re)=>{if(!t.doc)return e;let l=e;for(let s of t.doc)s.stmt==="dir"&&(l=s.value);return l},"getDir"),Pe=f(function(t,e){return e.db.getClasses()},"getClasses"),Be=f(async function(t,e,l,s){E.info("REF0:"),E.info("Drawing state diagram (v2)",e);let{securityLevel:c,state:h,layout:p}=$();s.db.extract(s.db.getRootDocV2());let y=s.db.getData(),o=Kt(e,c);y.type=s.type,y.layoutAlgorithm=p,y.nodeSpacing=h?.nodeSpacing||50,y.rankSpacing=h?.rankSpacing||50,$().look==="neo"?y.markers=["barbNeo"]:y.markers=["barb"],y.diagramId=e,await Ht(y,o);let k=8;try{(typeof s.db.getLinks=="function"?s.db.getLinks():new Map).forEach((L,D)=>{let d=typeof D=="string"?D:typeof D?.id=="string"?D.id:"",I=y.nodes.find(w=>w.id===d);if(!d){E.warn("\u26A0\uFE0F Invalid or missing stateId from key:",JSON.stringify(D));return}let F=o.node()?.querySelectorAll("g.node, g.rough-node"),A;if(F?.forEach(w=>{let z=w.textContent?.trim();(w.id===I?.domId||z===d)&&(A=w)}),!A){E.warn("\u26A0\uFE0F Could not find node matching text:",d);return}let P=A.parentNode;if(!P){E.warn("\u26A0\uFE0F Node has no parent, cannot wrap:",d);return}let x=document.createElementNS("http://www.w3.org/2000/svg","a"),B=L.url.replace(/^"+|"+$/g,"");if(x.setAttributeNS("http://www.w3.org/1999/xlink","xlink:href",B),x.setAttribute("target","_blank"),L.tooltip){let w=L.tooltip.replace(/^"+|"+$/g,"");x.setAttribute("title",w),A.setAttribute("title",w)}P.replaceChild(x,A),x.appendChild(A),E.info("\u{1F517} Wrapped node in <a> tag for:",d,L.url)})}catch(O){E.error("\u274C Error injecting clickable links:",O)}jt.insertTitle(o,"statediagramTitleText",h?.titleTopMargin??25,s.db.getDiagramTitle()),Xt(o,k,at,h?.useMaxWidth??!0)},"draw"),ts={getClasses:Pe,draw:Be,getDir:fe},bt=new Map,W=0;function it(t="",e=0,l="",s=It){let c=l!==null&&l.length>0?`${s}${l}`:"";return`${$e}-${t}${c}-${e}`}m(it,"stateDomId");f(it,"stateDomId");var Ye=f((t,e,l,s,c,h,p,y)=>{E.trace("items",e),e.forEach(o=>{switch(o.stmt){case Q:st(t,o,l,s,c,h,p,y);break;case rt:st(t,o,l,s,c,h,p,y);break;case Lt:{st(t,o.state1,l,s,c,h,p,y),st(t,o.state2,l,s,c,h,p,y);let b=p==="neo",k={id:"edge"+W,start:o.state1.id,end:o.state2.id,arrowhead:"normal",arrowTypeEnd:b?"arrow_barb_neo":"arrow_barb",style:ae,labelStyle:"",label:U.sanitizeText(o.description??"",$()),arrowheadStyle:ne,labelpos:oe,labelType:le,thickness:ce,classes:he,look:p};c.push(k),W++}break}})},"setupDoc"),te=f((t,e=re)=>{let l=e;if(t.doc)for(let s of t.doc)s.stmt==="dir"&&(l=s.value);return l},"getDir");function Z(t,e,l){if(!e.id||e.id==="</join></fork>"||e.id==="</choice>")return;e.cssClasses&&(Array.isArray(e.cssCompiledStyles)||(e.cssCompiledStyles=[]),e.cssClasses.split(" ").forEach(c=>{let h=l.get(c);h&&(e.cssCompiledStyles=[...e.cssCompiledStyles??[],...h.styles])}));let s=t.find(c=>c.id===e.id);s?Object.assign(s,e):t.push(e)}m(Z,"insertOrUpdateNode");f(Z,"insertOrUpdateNode");function wt(t){return t?.classes?.join(" ")??""}m(wt,"getClassesFromDbInfo");f(wt,"getClassesFromDbInfo");function Nt(t){return t?.styles??[]}m(Nt,"getStylesFromDbInfo");f(Nt,"getStylesFromDbInfo");var st=f((t,e,l,s,c,h,p,y)=>{let o=e.id,b=l.get(o),k=wt(b),O=Nt(b),L=$();if(E.info("dataFetcher parsedItem",e,b,O),o!=="root"){let D=Ct;e.start===!0?D=Ee:e.start===!1&&(D=ke),e.type!==rt&&(D=e.type),bt.get(o)||bt.set(o,{id:o,shape:D,description:U.sanitizeText(o,L),cssClasses:`${k} ${Ce}`,cssStyles:O});let d=bt.get(o);e.description&&(Array.isArray(d.description)?(d.shape=At,d.description.push(e.description)):d.description?.length&&d.description.length>0?(d.shape=At,d.description===o?d.description=[e.description]:d.description=[d.description,e.description]):(d.shape=Ct,d.description=e.description),d.description=U.sanitizeTextOrArray(d.description,L)),d.description?.length===1&&d.shape===At&&(d.type==="group"?d.shape=Qt:d.shape=Ct),!d.type&&e.doc&&(E.info("Setting cluster for XCX",o,te(e)),d.type="group",d.isGroup=!0,d.dir=te(e),d.shape=e.type===ie?qt:Qt,d.cssClasses=`${d.cssClasses} ${Ne} ${h?Re:""}`);let I={labelStyle:"",shape:d.shape,label:d.description,cssClasses:d.cssClasses,cssCompiledStyles:[],cssStyles:d.cssStyles,id:o,dir:d.dir,domId:it(o,W),type:d.type,isGroup:d.type==="group",padding:8,rx:10,ry:10,look:p,labelType:"markdown"};if(I.shape===qt&&(I.label=""),t&&t.id!=="root"&&(E.trace("Setting node ",o," to be child of its parent ",t.id),I.parentId=t.id),I.centerLabel=!0,e.note){let F={labelStyle:"",shape:_e,label:e.note.text,labelType:"markdown",cssClasses:Ie,cssStyles:[],cssCompiledStyles:[],id:o+Fe+"-"+W,domId:it(o,W,de),type:d.type,isGroup:d.type==="group",padding:L.flowchart?.padding,look:p,position:e.note.position},A=o+Zt,P={labelStyle:"",shape:De,label:e.note.text,cssClasses:d.cssClasses,cssStyles:[],id:o+Zt,domId:it(o,W,ue),type:"group",isGroup:!0,padding:16,look:p,position:e.note.position};W++,P.id=A,F.parentId=A,Z(s,P,y),Z(s,F,y),Z(s,I,y);let x=o,B=F.id;e.note.position==="left of"&&(x=F.id,B=o),c.push({id:x+"-"+B,start:x,end:B,arrowhead:"none",arrowTypeEnd:"",style:ae,labelStyle:"",classes:Le,arrowheadStyle:ne,labelpos:oe,labelType:le,thickness:ce,look:p})}else Z(s,I,y)}e.doc&&(E.trace("Adding nodes children "),Ye(e,e.doc,l,s,c,!h,p,y))},"dataFetcher"),Ge=f(()=>{bt.clear(),W=0},"reset"),C={START_NODE:"[*]",START_TYPE:"start",END_NODE:"[*]",END_TYPE:"end",COLOR_KEYWORD:"color",FILL_KEYWORD:"fill",BG_FILL:"bgFill",STYLECLASS_SEP:","},ee=f(()=>new Map,"newClassesList"),se=f(()=>({relations:[],states:new Map,documents:{}}),"newDoc"),gt=f(t=>JSON.parse(JSON.stringify(t)),"clone"),es=class{static{m(this,"StateDB")}constructor(t){this.version=t,this.nodes=[],this.edges=[],this.rootDoc=[],this.classes=ee(),this.documents={root:se()},this.currentDocument=this.documents.root,this.startEndCount=0,this.dividerCnt=0,this.links=new Map,this.funs=[],this.getAccTitle=Yt,this.setAccTitle=Bt,this.getAccDescription=Vt,this.setAccDescription=Gt,this.setDiagramTitle=Mt,this.getDiagramTitle=Ut,this.clear(),this.setRootDoc=this.setRootDoc.bind(this),this.getDividerId=this.getDividerId.bind(this),this.setDirection=this.setDirection.bind(this),this.trimColon=this.trimColon.bind(this),this.bindFunctions=this.bindFunctions.bind(this)}static{f(this,"StateDB")}static{this.relationType={AGGREGATION:0,EXTENSION:1,COMPOSITION:2,DEPENDENCY:3}}extract(t){this.clear(!0);for(let s of Array.isArray(t)?t:t.doc)switch(s.stmt){case Q:this.addState(s.id.trim(),s.type,s.doc,s.description,s.note);break;case Lt:this.addRelation(s.state1,s.state2,s.description);break;case be:this.addStyleClass(s.id.trim(),s.classes);break;case Te:this.handleStyleDef(s);break;case me:this.setCssClass(s.id.trim(),s.styleClass);break;case"click":this.addLink(s.id,s.url,s.tooltip);break}let e=this.getStates(),l=$();Ge(),st(void 0,this.getRootDocV2(),e,this.nodes,this.edges,!0,l.look,this.classes);for(let s of this.nodes)if(Array.isArray(s.label)){if(s.description=s.label.slice(1),s.isGroup&&s.description.length>0)throw new Error(`Group nodes can only have label. Remove the additional description for node [${s.id}]`);s.label=s.label[0]}}handleStyleDef(t){let e=t.id.trim().split(","),l=t.styleClass.split(",");for(let s of e){let c=this.getState(s);if(!c){let h=s.trim();this.addState(h),c=this.getState(h)}c&&(c.styles=l.map(h=>h.replace(/;/g,"")?.trim()))}}setRootDoc(t){E.info("Setting root doc",t),this.rootDoc=t,this.version===1?this.extract(t):this.extract(this.getRootDocV2())}docTranslator(t,e,l){if(e.stmt===Lt){this.docTranslator(t,e.state1,!0),this.docTranslator(t,e.state2,!1);return}if(e.stmt===Q&&(e.id===C.START_NODE?(e.id=t.id+(l?"_start":"_end"),e.start=l):e.id=e.id.trim()),e.stmt!==q&&e.stmt!==Q||!e.doc)return;let s=[],c=[];for(let h of e.doc)if(h.type===ie){let p=gt(h);p.doc=gt(c),s.push(p),c=[]}else c.push(h);if(s.length>0&&c.length>0){let h={stmt:Q,id:Wt(),type:"divider",doc:gt(c)};s.push(gt(h)),e.doc=s}e.doc.forEach(h=>this.docTranslator(e,h,!0))}getRootDocV2(){return this.docTranslator({id:q,stmt:q},{id:q,stmt:q,doc:this.rootDoc},!0),{id:q,doc:this.rootDoc}}addState(t,e=rt,l=void 0,s=void 0,c=void 0,h=void 0,p=void 0,y=void 0){let o=t?.trim();if(!this.currentDocument.states.has(o))E.info("Adding state ",o,s),this.currentDocument.states.set(o,{stmt:Q,id:o,descriptions:[],type:e,doc:l,note:c,classes:[],styles:[],textStyles:[]});else{let b=this.currentDocument.states.get(o);if(!b)throw new Error(`State not found: ${o}`);b.doc||(b.doc=l),b.type||(b.type=e)}if(s&&(E.info("Setting state description",o,s),(Array.isArray(s)?s:[s]).forEach(k=>this.addDescription(o,k.trim()))),c){let b=this.currentDocument.states.get(o);if(!b)throw new Error(`State not found: ${o}`);b.note=c,b.note.text=U.sanitizeText(b.note.text,$())}h&&(E.info("Setting state classes",o,h),(Array.isArray(h)?h:[h]).forEach(k=>this.setCssClass(o,k.trim()))),p&&(E.info("Setting state styles",o,p),(Array.isArray(p)?p:[p]).forEach(k=>this.setStyle(o,k.trim()))),y&&(E.info("Setting state styles",o,p),(Array.isArray(y)?y:[y]).forEach(k=>this.setTextStyle(o,k.trim())))}clear(t){this.nodes=[],this.edges=[],this.funs=[this.setupToolTips.bind(this)],this.documents={root:se()},this.currentDocument=this.documents.root,this.startEndCount=0,this.classes=ee(),t||(this.links=new Map,Pt())}getState(t){return this.currentDocument.states.get(t)}getStates(){return this.currentDocument.states}logDocuments(){E.info("Documents = ",this.documents)}getRelations(){return this.currentDocument.relations}addLink(t,e,l){this.links.set(t,{url:e,tooltip:l}),E.warn("Adding link",t,e,l)}getLinks(){return this.links}startIdIfNeeded(t=""){return t===C.START_NODE?(this.startEndCount++,`${C.START_TYPE}${this.startEndCount}`):t}startTypeIfNeeded(t="",e=rt){return t===C.START_NODE?C.START_TYPE:e}endIdIfNeeded(t=""){return t===C.END_NODE?(this.startEndCount++,`${C.END_TYPE}${this.startEndCount}`):t}endTypeIfNeeded(t="",e=rt){return t===C.END_NODE?C.END_TYPE:e}addRelationObjs(t,e,l=""){let s=this.startIdIfNeeded(t.id.trim()),c=this.startTypeIfNeeded(t.id.trim(),t.type),h=this.startIdIfNeeded(e.id.trim()),p=this.startTypeIfNeeded(e.id.trim(),e.type);this.addState(s,c,t.doc,t.description,t.note,t.classes,t.styles,t.textStyles),this.addState(h,p,e.doc,e.description,e.note,e.classes,e.styles,e.textStyles),this.currentDocument.relations.push({id1:s,id2:h,relationTitle:U.sanitizeText(l,$())})}addRelation(t,e,l){if(typeof t=="object"&&typeof e=="object")this.addRelationObjs(t,e,l);else if(typeof t=="string"&&typeof e=="string"){let s=this.startIdIfNeeded(t.trim()),c=this.startTypeIfNeeded(t),h=this.endIdIfNeeded(e.trim()),p=this.endTypeIfNeeded(e);this.addState(s,c),this.addState(h,p),this.currentDocument.relations.push({id1:s,id2:h,relationTitle:l?U.sanitizeText(l,$()):void 0})}}addDescription(t,e){let l=this.currentDocument.states.get(t),s=e.startsWith(":")?e.replace(":","").trim():e;l?.descriptions?.push(U.sanitizeText(s,$()))}cleanupLabel(t){return t.startsWith(":")?t.slice(2).trim():t.trim()}getDividerId(){return this.dividerCnt++,`divider-id-${this.dividerCnt}`}addStyleClass(t,e=""){this.classes.has(t)||this.classes.set(t,{id:t,styles:[],textStyles:[]});let l=this.classes.get(t);e&&l&&e.split(C.STYLECLASS_SEP).forEach(s=>{let c=s.replace(/([^;]*);/,"$1").trim();if(RegExp(C.COLOR_KEYWORD).exec(s)){let p=c.replace(C.FILL_KEYWORD,C.BG_FILL).replace(C.COLOR_KEYWORD,C.FILL_KEYWORD);l.textStyles.push(p)}l.styles.push(c)})}getClasses(){return this.classes}setupToolTips(t){let e=zt();yt(t).select("svg").selectAll("g.node, g.rough-node").on("mouseover",c=>{let h=yt(c.currentTarget),p=h.attr("title");if(p===null)return;let y=c.currentTarget?.getBoundingClientRect();e.transition().duration(200).style("opacity",".9"),e.style("left",window.scrollX+y.left+(y.right-y.left)/2+"px").style("top",window.scrollY+y.bottom+"px"),e.html(Ft.sanitize(p)),h.classed("hover",!0)}).on("mouseout",c=>{e.transition().duration(500).style("opacity",0),yt(c.currentTarget).classed("hover",!1)})}setCssClass(t,e){t.split(",").forEach(l=>{let s=this.getState(l);if(!s){let c=l.trim();this.addState(c),s=this.getState(c)}s?.classes?.push(e)})}setStyle(t,e){this.getState(t)?.styles?.push(e)}setTextStyle(t,e){this.getState(t)?.textStyles?.push(e)}bindFunctions(t){this.funs.forEach(e=>{e(t)})}getDirectionStatement(){return this.rootDoc.find(t=>t.stmt===Jt)}getDirection(){return this.getDirectionStatement()?.value??ge}setDirection(t){let e=this.getDirectionStatement();e?e.value=t:this.rootDoc.unshift({stmt:Jt,value:t})}trimColon(t){return t.startsWith(":")?t.slice(1).trim():t.trim()}getData(){let t=$();return{nodes:this.nodes,edges:this.edges,other:{},config:t,direction:fe(this.getRootDocV2())}}getConfig(){return $().state}},Ve=f(t=>`
defs [id$="-barbEnd"] {
    fill: ${t.transitionColor};
    stroke: ${t.transitionColor};
  }
g.stateGroup text {
  fill: ${t.nodeBorder};
  stroke: none;
  font-size: 10px;
}
g.stateGroup text {
  fill: ${t.textColor};
  stroke: none;
  font-size: 10px;

}
g.stateGroup .state-title {
  font-weight: bolder;
  fill: ${t.stateLabelColor};
}

g.stateGroup rect {
  fill: ${t.mainBkg};
  stroke: ${t.nodeBorder};
}

g.stateGroup line {
  stroke: ${t.lineColor};
  stroke-width: ${t.strokeWidth||1};
}

.transition {
  stroke: ${t.transitionColor};
  stroke-width: ${t.strokeWidth||1};
  fill: none;
}

.stateGroup .composit {
  fill: ${t.background};
  border-bottom: 1px
}

.stateGroup .alt-composit {
  fill: #e0e0e0;
  border-bottom: 1px
}

.state-note {
  stroke: ${t.noteBorderColor};
  fill: ${t.noteBkgColor};

  text {
    fill: ${t.noteTextColor};
    stroke: none;
    font-size: 10px;
  }
}

.stateLabel .box {
  stroke: none;
  stroke-width: 0;
  fill: ${t.mainBkg};
  opacity: 0.5;
}

.edgeLabel .label rect {
  fill: ${t.labelBackgroundColor};
  opacity: 0.5;
}
.edgeLabel {
  background-color: ${t.edgeLabelBackground};
  p {
    background-color: ${t.edgeLabelBackground};
  }
  rect {
    opacity: 0.5;
    background-color: ${t.edgeLabelBackground};
    fill: ${t.edgeLabelBackground};
  }
  text-align: center;
}
.edgeLabel .label text {
  fill: ${t.transitionLabelColor||t.tertiaryTextColor};
}
.label div .edgeLabel {
  color: ${t.transitionLabelColor||t.tertiaryTextColor};
}

.stateLabel text {
  fill: ${t.stateLabelColor};
  font-size: 10px;
  font-weight: bold;
}

.node circle.state-start {
  fill: ${t.specialStateColor};
  stroke: ${t.specialStateColor};
}

.node .fork-join {
  fill: ${t.specialStateColor};
  stroke: ${t.specialStateColor};
}

.node circle.state-end {
  fill: ${t.innerEndBackground};
  stroke: ${t.background};
  stroke-width: 1.5
}
.end-state-inner {
  fill: ${t.compositeBackground||t.background};
  // stroke: ${t.background};
  stroke-width: 1.5
}

.node rect {
  fill: ${t.stateBkg||t.mainBkg};
  stroke: ${t.stateBorder||t.nodeBorder};
  stroke-width: ${t.strokeWidth||1}px;
}
.node polygon {
  fill: ${t.mainBkg};
  stroke: ${t.stateBorder||t.nodeBorder};;
  stroke-width: ${t.strokeWidth||1}px;
}
[id$="-barbEnd"] {
  fill: ${t.lineColor};
}

.statediagram-cluster rect {
  fill: ${t.compositeTitleBackground};
  stroke: ${t.stateBorder||t.nodeBorder};
  stroke-width: ${t.strokeWidth||1}px;
}

.cluster-label, .nodeLabel {
  color: ${t.stateLabelColor};
  // line-height: 1;
}

.statediagram-cluster rect.outer {
  rx: 5px;
  ry: 5px;
}
.statediagram-state .divider {
  stroke: ${t.stateBorder||t.nodeBorder};
}

.statediagram-state .title-state {
  rx: 5px;
  ry: 5px;
}
.statediagram-cluster.statediagram-cluster .inner {
  fill: ${t.compositeBackground||t.background};
}
.statediagram-cluster.statediagram-cluster-alt .inner {
  fill: ${t.altBackground?t.altBackground:"#efefef"};
}

.statediagram-cluster .inner {
  rx:0;
  ry:0;
}

.statediagram-state rect.basic {
  rx: 5px;
  ry: 5px;
}
.statediagram-state rect.divider {
  stroke-dasharray: 10,10;
  fill: ${t.altBackground?t.altBackground:"#efefef"};
}

.note-edge {
  stroke-dasharray: 5;
}

.statediagram-note rect {
  fill: ${t.noteBkgColor};
  stroke: ${t.noteBorderColor};
  stroke-width: 1px;
  rx: 0;
  ry: 0;
}
.statediagram-note rect {
  fill: ${t.noteBkgColor};
  stroke: ${t.noteBorderColor};
  stroke-width: 1px;
  rx: 0;
  ry: 0;
}

.statediagram-note text {
  fill: ${t.noteTextColor};
}

.statediagram-note .nodeLabel {
  color: ${t.noteTextColor};
}
.statediagram .edgeLabel {
  color: red; // ${t.noteTextColor};
}

[id$="-dependencyStart"], [id$="-dependencyEnd"] {
  fill: ${t.lineColor};
  stroke: ${t.lineColor};
  stroke-width: ${t.strokeWidth||1};
}

.statediagramTitleText {
  text-anchor: middle;
  font-size: 18px;
  fill: ${t.textColor};
}

[data-look="neo"].statediagram-cluster rect {
  fill: ${t.mainBkg};
  stroke: ${t.useGradient?"url("+t.svgId+"-gradient)":t.stateBorder||t.nodeBorder};
  stroke-width: ${t.strokeWidth??1};
}
[data-look="neo"].statediagram-cluster rect.outer {
  rx: ${t.radius}px;
  ry: ${t.radius}px;
  filter: ${t.dropShadow?t.dropShadow.replace("url(#drop-shadow)",`url(${t.svgId}-drop-shadow)`):"none"}
}
`,"getStyles"),ss=Ve;export{Ze as a,ts as b,es as c,ss as d};
