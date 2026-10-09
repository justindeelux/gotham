import{d as q,o as r,c as l,a as d,ak as S,al as s,a3 as f,F as D,x as k,g as w,am as A,an as M,ao as L,ap as X,aq as H,ar as Y,$ as I,a0 as c,as as B,a1 as V,at as O,a4 as E,au as J,av as K,a6 as G}from"./index-WdU1ywSF.js";import{f as N}from"./format-length-B-p6aW7q.js";const Z=["id"],Q=["stop-color"],U=["stop-color"],ee=["viewBox"],re=["d","stroke-width"],te=["d","stroke-width"],ie={success:(r(),k(X)),error:(r(),k(L)),warning:(r(),k(M)),info:(r(),k(A))};var oe=q({name:"ProgressCircle",props:{clsPrefix:{type:String,required:!0},status:{type:String,required:!0},strokeWidth:{type:Number,required:!0},fillColor:[String,Object],railColor:String,railStyle:[String,Object],percentage:{type:Number,default:0},offsetDegree:{type:Number,default:0},showIndicator:{type:Boolean,required:!0},indicatorTextColor:String,unit:String,viewBoxWidth:{type:Number,required:!0},gapDegree:{type:Number,required:!0},gapOffsetDegree:{type:Number,default:0}},setup(e,{slots:y}){const h=w(()=>{const t="gradient",{fillColor:o}=e;return typeof o=="object"?`${t}-${H(JSON.stringify(o))}`:t});function x(t,o,g,p){const{gapDegree:m,viewBoxWidth:v,strokeWidth:b}=e,a=50,$=0,n=a,i=0,_=100,P=50+b/2,C=`M ${P},${P} m ${$},${n}
      a ${a},${a} 0 1 1 ${i},-100
      a ${a},${a} 0 1 1 0,${_}`,z=Math.PI*2*a;return{pathString:C,pathStyle:{stroke:p==="rail"?g:typeof e.fillColor=="object"?`url(#${h.value})`:g,strokeDasharray:`${Math.min(t,100)/100*(z-m)}px ${v*8}px`,strokeDashoffset:`-${m/2}px`,transformOrigin:o?"center":void 0,transform:o?`rotate(${o}deg)`:void 0}}}const u=()=>{const t=typeof e.fillColor=="object",o=t?e.fillColor.stops[0]:"",g=t?e.fillColor.stops[1]:"";return t&&(r(),l("defs",null,[d("linearGradient",{id:h.value,x1:"0%",y1:"100%",x2:"100%",y2:"0%"},[d("stop",{offset:"0%","stop-color":o},null,8,Q),d("stop",{offset:"100%","stop-color":g},null,8,U)],8,Z)]))};return()=>{const{fillColor:t,railColor:o,strokeWidth:g,offsetDegree:p,status:m,percentage:v,showIndicator:b,indicatorTextColor:a,unit:$,gapOffsetDegree:n,clsPrefix:i}=e,{pathString:_,pathStyle:P}=x(100,0,o,"rail"),{pathString:C,pathStyle:z}=x(v,p,t,"fill"),R=100+g;return r(),l("div",{class:s(`${i}-progress-content`),role:"none"},[d("div",{class:s(`${i}-progress-graph`),"aria-hidden":!0},[d("div",{class:s(`${i}-progress-graph-circle`),style:S({transform:n?`rotate(${n}deg)`:void 0})},[(r(),l("svg",{viewBox:`0 0 ${R} ${R}`},[f(()=>u()),d("g",null,[d("path",{class:s(`${i}-progress-graph-circle-rail`),d:_,"stroke-width":g,"stroke-linecap":"round",fill:"none",style:S(P)},null,14,re)]),d("g",null,[d("path",{class:s([`${i}-progress-graph-circle-fill`,v===0&&`${i}-progress-graph-circle-fill--empty`]),d:C,"stroke-width":g,"stroke-linecap":"round",fill:"none",style:S(z)},null,14,te)])],8,ee))],6)],2),b?(r(),l("div",{key:0},[y.default?(r(),l("div",{key:0,class:s(`${i}-progress-custom-content`),role:"none"},[f(()=>y.default())],2)):(r(),l(D,{key:1},[m!=="default"?(r(),l("div",{key:0,class:s(`${i}-progress-icon`),"aria-hidden":!0},[(r(),k(Y,{clsPrefix:i},{default:()=>ie[m]},1032,["clsPrefix"]))],2)):(r(),l("div",{key:1,class:s(`${i}-progress-text`),style:S({color:a}),role:"none"},[d("span",{class:s(`${i}-progress-text__percentage`)},[f(()=>v)],2),d("span",{class:s(`${i}-progress-text__unit`)},[f(()=>$)],2)],6))],64))])):f(()=>null)],2)}}});const se={success:(r(),k(X)),error:(r(),k(L)),warning:(r(),k(M)),info:(r(),k(A))};var le=q({name:"ProgressLine",props:{clsPrefix:{type:String,required:!0},percentage:{type:Number,default:0},railColor:String,railStyle:[String,Object],fillColor:[String,Object],status:{type:String,required:!0},indicatorPlacement:{type:String,required:!0},indicatorTextColor:String,unit:{type:String,default:"%"},processing:{type:Boolean,required:!0},showIndicator:{type:Boolean,required:!0},height:[String,Number],railBorderRadius:[String,Number],fillBorderRadius:[String,Number]},setup(e,{slots:y}){const h=w(()=>N(e.height)),x=w(()=>typeof e.fillColor=="object"?`linear-gradient(to right, ${e.fillColor?.stops[0]} , ${e.fillColor?.stops[1]})`:e.fillColor),u=w(()=>e.railBorderRadius!==void 0?N(e.railBorderRadius):e.height!==void 0?N(e.height,{c:.5}):""),t=w(()=>e.fillBorderRadius!==void 0?N(e.fillBorderRadius):e.railBorderRadius!==void 0?N(e.railBorderRadius):e.height!==void 0?N(e.height,{c:.5}):"");return()=>{const{indicatorPlacement:o,railColor:g,railStyle:p,percentage:m,unit:v,indicatorTextColor:b,status:a,showIndicator:$,processing:n,clsPrefix:i}=e;return r(),l("div",{class:s(`${i}-progress-content`),role:"none"},[d("div",{class:s(`${i}-progress-graph`),"aria-hidden":!0},[d("div",{class:s([`${i}-progress-graph-line`,{[`${i}-progress-graph-line--indicator-${o}`]:!0}])},[d("div",{class:s(`${i}-progress-graph-line-rail`),style:S([{backgroundColor:g,height:h.value,borderRadius:u.value},p])},[d("div",{class:s([`${i}-progress-graph-line-fill`,n&&`${i}-progress-graph-line-fill--processing`]),style:S({maxWidth:`${e.percentage}%`,background:x.value,height:h.value,lineHeight:h.value,borderRadius:t.value})},[o==="inside"?(r(),l("div",{key:0,class:s(`${i}-progress-graph-line-indicator`),style:S({color:b})},[y.default?(r(),l(D,{key:0},[f(()=>y.default())],64)):(r(),l(D,{key:1},[f(()=>`${m}${v}`)],64))],6)):f(()=>null)],6)],6)],2)],2),$&&o==="outside"?(r(),l("div",{key:0},[y.default?(r(),l("div",{key:0,class:s(`${i}-progress-custom-content`),style:S({color:b}),role:"none"},[f(()=>y.default())],6)):(r(),l(D,{key:1},[a==="default"?(r(),l("div",{key:0,role:"none",class:s(`${i}-progress-icon ${i}-progress-icon--as-text`),style:S({color:b})},[f(()=>m),f(()=>v)],6)):(r(),l("div",{key:1,class:s(`${i}-progress-icon`),"aria-hidden":!0},[(r(),k(Y,{clsPrefix:i},{default:()=>se[a]},1032,["clsPrefix"]))],2))],64))])):f(()=>null)],2)}}});const ae=["id"],ne=["stop-color"],ce=["stop-color"],de=["d","stroke-width"],ge=["d","stroke-width"],ue=["viewBox"];function T(e,y,h=100){return`m ${h/2} ${h/2-e} a ${e} ${e} 0 1 1 0 ${2*e} a ${e} ${e} 0 1 1 0 -${2*e}`}var pe=q({name:"ProgressMultipleCircle",props:{clsPrefix:{type:String,required:!0},viewBoxWidth:{type:Number,required:!0},percentage:{type:Array,default:[0]},strokeWidth:{type:Number,required:!0},circleGap:{type:Number,required:!0},showIndicator:{type:Boolean,required:!0},fillColor:{type:Array,default:()=>[]},railColor:{type:Array,default:()=>[]},railStyle:{type:Array,default:()=>[]}},setup(e,{slots:y}){const h=w(()=>e.percentage.map((u,t)=>`${Math.PI*u/100*(e.viewBoxWidth/2-e.strokeWidth/2*(1+2*t)-e.circleGap*t)*2}, ${e.viewBoxWidth*8}`)),x=(u,t)=>{const o=e.fillColor[t],g=typeof o=="object"?o.stops[0]:"",p=typeof o=="object"?o.stops[1]:"";return typeof e.fillColor[t]=="object"&&(r(),l("linearGradient",{id:`gradient-${t}`,x1:"100%",y1:"0%",x2:"0%",y2:"100%"},[d("stop",{offset:"0%","stop-color":g},null,8,ne),d("stop",{offset:"100%","stop-color":p},null,8,ce)],8,ae))};return()=>{const{viewBoxWidth:u,strokeWidth:t,circleGap:o,showIndicator:g,fillColor:p,railColor:m,railStyle:v,percentage:b,clsPrefix:a}=e;return r(),l("div",{class:s(`${a}-progress-content`),role:"none"},[d("div",{class:s(`${a}-progress-graph`),"aria-hidden":!0},[d("div",{class:s(`${a}-progress-graph-circle`)},[(r(),l("svg",{viewBox:`0 0 ${u} ${u}`},[d("defs",null,[f(()=>b.map(($,n)=>x($,n)))]),f(()=>b.map(($,n)=>(r(),l("g",{key:n},[d("path",{class:s(`${a}-progress-graph-circle-rail`),d:T(u/2-t/2*(1+2*n)-o*n,t,u),"stroke-width":t,"stroke-linecap":"round",fill:"none",style:S([{strokeDashoffset:0,stroke:m[n]},v[n]])},null,14,de),d("path",{class:s([`${a}-progress-graph-circle-fill`,$===0&&`${a}-progress-graph-circle-fill--empty`]),d:T(u/2-t/2*(1+2*n)-o*n,t,u),"stroke-width":t,"stroke-linecap":"round",fill:"none",style:S({strokeDasharray:h.value[n],strokeDashoffset:0,stroke:typeof p[n]=="object"?`url(#gradient-${n})`:p[n]})},null,14,ge)]))))],8,ue))],2)],2),g&&y.default?(r(),l("div",{key:0},[d("div",{class:s(`${a}-progress-text`)},[f(()=>y.default())],2)])):f(()=>null)],2)}}}),fe=I([c("progress",{display:"inline-block"},[c("progress-icon",`
 color: var(--n-icon-color);
 transition: color .3s var(--n-bezier);
 `),B("line",`
 width: 100%;
 display: block;
 `,[c("progress-content",`
 display: flex;
 align-items: center;
 `,[c("progress-graph",{flex:1})]),c("progress-custom-content",{marginLeft:"14px"}),c("progress-icon",`
 width: 30px;
 padding-left: 14px;
 height: var(--n-icon-size-line);
 line-height: var(--n-icon-size-line);
 font-size: var(--n-icon-size-line);
 `,[B("as-text",`
 color: var(--n-text-color-line-outer);
 text-align: center;
 width: 40px;
 font-size: var(--n-font-size);
 padding-left: 4px;
 transition: color .3s var(--n-bezier);
 `)])]),B("circle, dashboard",{width:"120px"},[c("progress-custom-content",`
 position: absolute;
 left: 50%;
 top: 50%;
 transform: translateX(-50%) translateY(-50%);
 display: flex;
 align-items: center;
 justify-content: center;
 `),c("progress-text",`
 position: absolute;
 left: 50%;
 top: 50%;
 transform: translateX(-50%) translateY(-50%);
 display: flex;
 align-items: center;
 color: inherit;
 font-size: var(--n-font-size-circle);
 color: var(--n-text-color-circle);
 font-weight: var(--n-font-weight-circle);
 transition: color .3s var(--n-bezier);
 white-space: nowrap;
 `),c("progress-icon",`
 position: absolute;
 left: 50%;
 top: 50%;
 transform: translateX(-50%) translateY(-50%);
 display: flex;
 align-items: center;
 color: var(--n-icon-color);
 font-size: var(--n-icon-size-circle);
 `)]),B("multiple-circle",`
 width: 200px;
 color: inherit;
 `,[c("progress-text",`
 font-weight: var(--n-font-weight-circle);
 color: var(--n-text-color-circle);
 position: absolute;
 left: 50%;
 top: 50%;
 transform: translateX(-50%) translateY(-50%);
 display: flex;
 align-items: center;
 justify-content: center;
 transition: color .3s var(--n-bezier);
 `)]),c("progress-content",{position:"relative"}),c("progress-graph",{position:"relative"},[c("progress-graph-circle",[I("svg",{verticalAlign:"bottom"}),c("progress-graph-circle-fill",`
 stroke: var(--n-fill-color);
 transition:
 opacity .3s var(--n-bezier),
 stroke .3s var(--n-bezier),
 stroke-dasharray .3s var(--n-bezier);
 `,[B("empty",{opacity:0})]),c("progress-graph-circle-rail",`
 transition: stroke .3s var(--n-bezier);
 overflow: hidden;
 stroke: var(--n-rail-color);
 `)]),c("progress-graph-line",[B("indicator-inside",[c("progress-graph-line-rail",`
 height: 16px;
 line-height: 16px;
 border-radius: 10px;
 `,[c("progress-graph-line-fill",`
 height: inherit;
 border-radius: 10px;
 `),c("progress-graph-line-indicator",`
 background: #0000;
 white-space: nowrap;
 text-align: right;
 margin-left: 14px;
 margin-right: 14px;
 height: inherit;
 font-size: 12px;
 color: var(--n-text-color-line-inner);
 transition: color .3s var(--n-bezier);
 `)])]),B("indicator-inside-label",`
 height: 16px;
 display: flex;
 align-items: center;
 `,[c("progress-graph-line-rail",`
 flex: 1;
 transition: background-color .3s var(--n-bezier);
 `),c("progress-graph-line-indicator",`
 background: var(--n-fill-color);
 font-size: 12px;
 transform: translateZ(0);
 display: flex;
 vertical-align: middle;
 height: 16px;
 line-height: 16px;
 padding: 0 10px;
 border-radius: 10px;
 position: absolute;
 white-space: nowrap;
 color: var(--n-text-color-line-inner);
 transition:
 right .2s var(--n-bezier),
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier);
 `)]),c("progress-graph-line-rail",`
 position: relative;
 overflow: hidden;
 height: var(--n-rail-height);
 border-radius: 5px;
 background-color: var(--n-rail-color);
 transition: background-color .3s var(--n-bezier);
 `,[c("progress-graph-line-fill",`
 background: var(--n-fill-color);
 position: relative;
 border-radius: 5px;
 height: inherit;
 width: 100%;
 max-width: 0%;
 transition:
 background-color .3s var(--n-bezier),
 max-width .2s var(--n-bezier);
 `,[B("processing",[I("&::after",`
 content: "";
 background-image: var(--n-line-bg-processing);
 animation: progress-processing-animation 2s var(--n-bezier) infinite;
 `)])])])])])]),I("@keyframes progress-processing-animation",`
 0% {
 position: absolute;
 left: 0;
 top: 0;
 bottom: 0;
 right: 100%;
 opacity: 1;
 }
 66% {
 position: absolute;
 left: 0;
 top: 0;
 bottom: 0;
 right: 0;
 opacity: 0;
 }
 100% {
 position: absolute;
 left: 0;
 top: 0;
 bottom: 0;
 right: 0;
 opacity: 0;
 }
 `)]);const he=["aria-valuenow","role"],ye={...V.props,processing:Boolean,type:{type:String,default:"line"},gapDegree:Number,gapOffsetDegree:Number,status:{type:String,default:"default"},railColor:[String,Array],railStyle:[String,Array],color:[String,Array,Object],viewBoxWidth:{type:Number,default:100},strokeWidth:{type:Number,default:7},percentage:[Number,Array],unit:{type:String,default:"%"},showIndicator:{type:Boolean,default:!0},indicatorPosition:{type:String,default:"outside"},indicatorPlacement:{type:String,default:"outside"},indicatorTextColor:String,circleGap:{type:Number,default:1},height:Number,borderRadius:[String,Number],fillBorderRadius:[String,Number],offsetDegree:Number};var be=q({name:"Progress",props:ye,setup(e){const y=w(()=>e.indicatorPlacement||e.indicatorPosition),h=w(()=>{if(e.gapDegree||e.gapDegree===0)return e.gapDegree;if(e.type==="dashboard")return 75}),{mergedClsPrefixRef:x,inlineThemeDisabled:u}=E(e),t=V("Progress","-progress",fe,K,e,x),o=w(()=>{const{status:p}=e,{common:{cubicBezierEaseInOut:m},self:{fontSize:v,fontSizeCircle:b,railColor:a,railHeight:$,iconSizeCircle:n,iconSizeLine:i,textColorCircle:_,textColorLineInner:P,textColorLineOuter:C,lineBgProcessing:z,fontWeightCircle:R,[G("iconColor",p)]:j,[G("fillColor",p)]:W}}=t.value;return{"--n-bezier":m,"--n-fill-color":W,"--n-font-size":v,"--n-font-size-circle":b,"--n-font-weight-circle":R,"--n-icon-color":j,"--n-icon-size-circle":n,"--n-icon-size-line":i,"--n-line-bg-processing":z,"--n-rail-color":a,"--n-rail-height":$,"--n-text-color-circle":_,"--n-text-color-line-inner":P,"--n-text-color-line-outer":C}}),g=u?J("progress",w(()=>e.status[0]),o,e):void 0;return{mergedClsPrefix:x,mergedIndicatorPlacement:y,gapDeg:h,cssVars:u?void 0:o,themeClass:g?.themeClass,onRender:g?.onRender}},render(){const{type:e,cssVars:y,indicatorTextColor:h,showIndicator:x,status:u,railColor:t,railStyle:o,color:g,percentage:p,viewBoxWidth:m,strokeWidth:v,mergedIndicatorPlacement:b,unit:a,borderRadius:$,fillBorderRadius:n,height:i,processing:_,circleGap:P,mergedClsPrefix:C,gapDeg:z,gapOffsetDegree:R,themeClass:j,$slots:W,onRender:F}=this;return F?.(),r(),l("div",{class:s([j,`${C}-progress`,`${C}-progress--${e}`,`${C}-progress--${u}`]),style:S(y),"aria-valuemax":100,"aria-valuemin":0,"aria-valuenow":p,role:e==="circle"||e==="line"||e==="dashboard"?"progressbar":"none"},[e==="circle"||e==="dashboard"?(r(),k(oe,{key:0,clsPrefix:C,status:u,showIndicator:x,indicatorTextColor:h,railColor:t,fillColor:g,railStyle:o,offsetDegree:this.offsetDegree,percentage:p,viewBoxWidth:m,strokeWidth:v,gapDegree:z===void 0?e==="dashboard"?75:0:z,gapOffsetDegree:R,unit:a},O(W),1032,["clsPrefix","status","showIndicator","indicatorTextColor","railColor","fillColor","railStyle","offsetDegree","percentage","viewBoxWidth","strokeWidth","gapDegree","gapOffsetDegree","unit"])):(r(),l(D,{key:1},[e==="line"?(r(),k(le,{key:0,clsPrefix:C,status:u,showIndicator:x,indicatorTextColor:h,railColor:t,fillColor:g,railStyle:o,percentage:p,processing:_,indicatorPlacement:b,unit:a,fillBorderRadius:n,railBorderRadius:$,height:i},O(W),1032,["clsPrefix","status","showIndicator","indicatorTextColor","railColor","fillColor","railStyle","percentage","processing","indicatorPlacement","unit","fillBorderRadius","railBorderRadius","height"])):(r(),l(D,{key:1},[e==="multiple-circle"?(r(),k(pe,{key:0,clsPrefix:C,strokeWidth:v,railColor:t,fillColor:g,railStyle:o,viewBoxWidth:m,percentage:p,showIndicator:x,circleGap:P},O(W),1032,["clsPrefix","strokeWidth","railColor","fillColor","railStyle","viewBoxWidth","percentage","showIndicator","circleGap"])):f(()=>null)],64))],64))],14,he)}});export{be as P};
