import{E as j}from"./Empty-Dfzv9qbi.js";import{T as Y}from"./Tag-bZAMy3FD.js";import{A as te}from"./Alert-DE2JjdOV.js";import{E as he,d as E,o as s,c as p,a as n,G as N,H as v,I as z,F as A,m,y as B,J as ne,W as de,K as ce,S as ue,L as me,M as pe,N as H,O as _,P as O,Q as U,T as F,U as ge,V as ye,X as ve,Y as ee,Z as be,_ as ke,$ as _e,a0 as xe,a1 as re,g as $e,A as Se,e as l,w as a,u as t,j as Z,l as M,C as q,r as J,B as K,k as f,t as P}from"./index-C37tQAIO.js";import{S as V}from"./Space-DJUBMDOt.js";import{f as G}from"./format-length-CaNGVLgz.js";import{t as w}from"./text-BVGBNUO3.js";import{S as ie}from"./ServerStatusTag-qCYX-ie2.js";import{u as Ce}from"./servers-DiXXd572.js";import{r as se}from"./format-BJCKbznv.js";import{_ as we}from"./_plugin-vue_export-helper-DlAUqK2U.js";import"./servers-j5__v7d6.js";let oe=!1;function ze(){if(he&&window.CSS&&!oe&&(oe=!0,"registerProperty"in window?.CSS))try{CSS.registerProperty({name:"--n-color-start",syntax:"<color>",inherits:!1,initialValue:"#0000"}),CSS.registerProperty({name:"--n-color-end",syntax:"<color>",inherits:!1,initialValue:"#0000"})}catch{}}const Pe=["id"],Be=["stop-color"],Re=["stop-color"],Ne=["viewBox"],De=["d","stroke-width"],We=["d","stroke-width"],Ie={success:(s(),m(ue)),error:(s(),m(ce)),warning:(s(),m(de)),info:(s(),m(ne))};var Ae=E({name:"ProgressCircle",props:{clsPrefix:{type:String,required:!0},status:{type:String,required:!0},strokeWidth:{type:Number,required:!0},fillColor:[String,Object],railColor:String,railStyle:[String,Object],percentage:{type:Number,default:0},offsetDegree:{type:Number,default:0},showIndicator:{type:Boolean,required:!0},indicatorTextColor:String,unit:String,viewBoxWidth:{type:Number,required:!0},gapDegree:{type:Number,required:!0},gapOffsetDegree:{type:Number,default:0}},setup(e,{slots:k}){const g=B(()=>{const d="gradient",{fillColor:c}=e;return typeof c=="object"?`${d}-${me(JSON.stringify(c))}`:d});function b(d,c,y,$){const{gapDegree:x,viewBoxWidth:C,strokeWidth:S}=e,o=50,r=0,i=o,u=0,D=100,W=50+S/2,R=`M ${W},${W} m ${r},${i}
      a ${o},${o} 0 1 1 ${u},-100
      a ${o},${o} 0 1 1 0,${D}`,I=Math.PI*2*o;return{pathString:R,pathStyle:{stroke:$==="rail"?y:typeof e.fillColor=="object"?`url(#${g.value})`:y,strokeDasharray:`${Math.min(d,100)/100*(I-x)}px ${C*8}px`,strokeDashoffset:`-${x/2}px`,transformOrigin:c?"center":void 0,transform:c?`rotate(${c}deg)`:void 0}}}const h=()=>{const d=typeof e.fillColor=="object",c=d?e.fillColor.stops[0]:"",y=d?e.fillColor.stops[1]:"";return d&&(s(),p("defs",null,[n("linearGradient",{id:g.value,x1:"0%",y1:"100%",x2:"100%",y2:"0%"},[n("stop",{offset:"0%","stop-color":c},null,8,Be),n("stop",{offset:"100%","stop-color":y},null,8,Re)],8,Pe)]))};return()=>{const{fillColor:d,railColor:c,strokeWidth:y,offsetDegree:$,status:x,percentage:C,showIndicator:S,indicatorTextColor:o,unit:r,gapOffsetDegree:i,clsPrefix:u}=e,{pathString:D,pathStyle:W}=b(100,0,c,"rail"),{pathString:R,pathStyle:I}=b(C,$,d,"fill"),T=100+y;return s(),p("div",{class:v(`${u}-progress-content`),role:"none"},[n("div",{class:v(`${u}-progress-graph`),"aria-hidden":!0},[n("div",{class:v(`${u}-progress-graph-circle`),style:N({transform:i?`rotate(${i}deg)`:void 0})},[(s(),p("svg",{viewBox:`0 0 ${T} ${T}`},[z(()=>h()),n("g",null,[n("path",{class:v(`${u}-progress-graph-circle-rail`),d:D,"stroke-width":y,"stroke-linecap":"round",fill:"none",style:N(W)},null,14,De)]),n("g",null,[n("path",{class:v([`${u}-progress-graph-circle-fill`,C===0&&`${u}-progress-graph-circle-fill--empty`]),d:R,"stroke-width":y,"stroke-linecap":"round",fill:"none",style:N(I)},null,14,We)])],8,Ne))],6)],2),S?(s(),p("div",{key:0},[k.default?(s(),p("div",{key:0,class:v(`${u}-progress-custom-content`),role:"none"},[z(()=>k.default())],2)):(s(),p(A,{key:1},[x!=="default"?(s(),p("div",{key:0,class:v(`${u}-progress-icon`),"aria-hidden":!0},[(s(),m(pe,{clsPrefix:u},{default:()=>Ie[x]},1032,["clsPrefix"]))],2)):(s(),p("div",{key:1,class:v(`${u}-progress-text`),style:N({color:o}),role:"none"},[n("span",{class:v(`${u}-progress-text__percentage`)},[z(()=>C)],2),n("span",{class:v(`${u}-progress-text__unit`)},[z(()=>r)],2)],6))],64))])):z(()=>null)],2)}}});const Te={success:(s(),m(ue)),error:(s(),m(ce)),warning:(s(),m(de)),info:(s(),m(ne))};var qe=E({name:"ProgressLine",props:{clsPrefix:{type:String,required:!0},percentage:{type:Number,default:0},railColor:String,railStyle:[String,Object],fillColor:[String,Object],status:{type:String,required:!0},indicatorPlacement:{type:String,required:!0},indicatorTextColor:String,unit:{type:String,default:"%"},processing:{type:Boolean,required:!0},showIndicator:{type:Boolean,required:!0},height:[String,Number],railBorderRadius:[String,Number],fillBorderRadius:[String,Number]},setup(e,{slots:k}){const g=B(()=>G(e.height)),b=B(()=>typeof e.fillColor=="object"?`linear-gradient(to right, ${e.fillColor?.stops[0]} , ${e.fillColor?.stops[1]})`:e.fillColor),h=B(()=>e.railBorderRadius!==void 0?G(e.railBorderRadius):e.height!==void 0?G(e.height,{c:.5}):""),d=B(()=>e.fillBorderRadius!==void 0?G(e.fillBorderRadius):e.railBorderRadius!==void 0?G(e.railBorderRadius):e.height!==void 0?G(e.height,{c:.5}):"");return()=>{const{indicatorPlacement:c,railColor:y,railStyle:$,percentage:x,unit:C,indicatorTextColor:S,status:o,showIndicator:r,processing:i,clsPrefix:u}=e;return s(),p("div",{class:v(`${u}-progress-content`),role:"none"},[n("div",{class:v(`${u}-progress-graph`),"aria-hidden":!0},[n("div",{class:v([`${u}-progress-graph-line`,{[`${u}-progress-graph-line--indicator-${c}`]:!0}])},[n("div",{class:v(`${u}-progress-graph-line-rail`),style:N([{backgroundColor:y,height:g.value,borderRadius:h.value},$])},[n("div",{class:v([`${u}-progress-graph-line-fill`,i&&`${u}-progress-graph-line-fill--processing`]),style:N({maxWidth:`${e.percentage}%`,background:b.value,height:g.value,lineHeight:g.value,borderRadius:d.value})},[c==="inside"?(s(),p("div",{key:0,class:v(`${u}-progress-graph-line-indicator`),style:N({color:S})},[k.default?(s(),p(A,{key:0},[z(()=>k.default())],64)):(s(),p(A,{key:1},[z(()=>`${x}${C}`)],64))],6)):z(()=>null)],6)],6)],2)],2),r&&c==="outside"?(s(),p("div",{key:0},[k.default?(s(),p("div",{key:0,class:v(`${u}-progress-custom-content`),style:N({color:S}),role:"none"},[z(()=>k.default())],6)):(s(),p(A,{key:1},[o==="default"?(s(),p("div",{key:0,role:"none",class:v(`${u}-progress-icon ${u}-progress-icon--as-text`),style:N({color:S})},[z(()=>x),z(()=>C)],6)):(s(),p("div",{key:1,class:v(`${u}-progress-icon`),"aria-hidden":!0},[(s(),m(pe,{clsPrefix:u},{default:()=>Te[o]},1032,["clsPrefix"]))],2))],64))])):z(()=>null)],2)}}});const je=["id"],Oe=["stop-color"],Le=["stop-color"],Me=["d","stroke-width"],Ve=["d","stroke-width"],Ge=["viewBox"];function ae(e,k,g=100){return`m ${g/2} ${g/2-e} a ${e} ${e} 0 1 1 0 ${2*e} a ${e} ${e} 0 1 1 0 -${2*e}`}var He=E({name:"ProgressMultipleCircle",props:{clsPrefix:{type:String,required:!0},viewBoxWidth:{type:Number,required:!0},percentage:{type:Array,default:[0]},strokeWidth:{type:Number,required:!0},circleGap:{type:Number,required:!0},showIndicator:{type:Boolean,required:!0},fillColor:{type:Array,default:()=>[]},railColor:{type:Array,default:()=>[]},railStyle:{type:Array,default:()=>[]}},setup(e,{slots:k}){const g=B(()=>e.percentage.map((h,d)=>`${Math.PI*h/100*(e.viewBoxWidth/2-e.strokeWidth/2*(1+2*d)-e.circleGap*d)*2}, ${e.viewBoxWidth*8}`)),b=(h,d)=>{const c=e.fillColor[d],y=typeof c=="object"?c.stops[0]:"",$=typeof c=="object"?c.stops[1]:"";return typeof e.fillColor[d]=="object"&&(s(),p("linearGradient",{id:`gradient-${d}`,x1:"100%",y1:"0%",x2:"0%",y2:"100%"},[n("stop",{offset:"0%","stop-color":y},null,8,Oe),n("stop",{offset:"100%","stop-color":$},null,8,Le)],8,je))};return()=>{const{viewBoxWidth:h,strokeWidth:d,circleGap:c,showIndicator:y,fillColor:$,railColor:x,railStyle:C,percentage:S,clsPrefix:o}=e;return s(),p("div",{class:v(`${o}-progress-content`),role:"none"},[n("div",{class:v(`${o}-progress-graph`),"aria-hidden":!0},[n("div",{class:v(`${o}-progress-graph-circle`)},[(s(),p("svg",{viewBox:`0 0 ${h} ${h}`},[n("defs",null,[z(()=>S.map((r,i)=>b(r,i)))]),z(()=>S.map((r,i)=>(s(),p("g",{key:i},[n("path",{class:v(`${o}-progress-graph-circle-rail`),d:ae(h/2-d/2*(1+2*i)-c*i,d,h),"stroke-width":d,"stroke-linecap":"round",fill:"none",style:N([{strokeDashoffset:0,stroke:x[i]},C[i]])},null,14,Me),n("path",{class:v([`${o}-progress-graph-circle-fill`,r===0&&`${o}-progress-graph-circle-fill--empty`]),d:ae(h/2-d/2*(1+2*i)-c*i,d,h),"stroke-width":d,"stroke-linecap":"round",fill:"none",style:N({strokeDasharray:g.value[i],strokeDashoffset:0,stroke:typeof $[i]=="object"?`url(#gradient-${i})`:$[i]})},null,14,Ve)]))))],8,Ge))],2)],2),y&&k.default?(s(),p("div",{key:0},[n("div",{class:v(`${o}-progress-text`)},[z(()=>k.default())],2)])):z(()=>null)],2)}}}),Ee=H([_("progress",{display:"inline-block"},[_("progress-icon",`
 color: var(--n-icon-color);
 transition: color .3s var(--n-bezier);
 `),O("line",`
 width: 100%;
 display: block;
 `,[_("progress-content",`
 display: flex;
 align-items: center;
 `,[_("progress-graph",{flex:1})]),_("progress-custom-content",{marginLeft:"14px"}),_("progress-icon",`
 width: 30px;
 padding-left: 14px;
 height: var(--n-icon-size-line);
 line-height: var(--n-icon-size-line);
 font-size: var(--n-icon-size-line);
 `,[O("as-text",`
 color: var(--n-text-color-line-outer);
 text-align: center;
 width: 40px;
 font-size: var(--n-font-size);
 padding-left: 4px;
 transition: color .3s var(--n-bezier);
 `)])]),O("circle, dashboard",{width:"120px"},[_("progress-custom-content",`
 position: absolute;
 left: 50%;
 top: 50%;
 transform: translateX(-50%) translateY(-50%);
 display: flex;
 align-items: center;
 justify-content: center;
 `),_("progress-text",`
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
 `),_("progress-icon",`
 position: absolute;
 left: 50%;
 top: 50%;
 transform: translateX(-50%) translateY(-50%);
 display: flex;
 align-items: center;
 color: var(--n-icon-color);
 font-size: var(--n-icon-size-circle);
 `)]),O("multiple-circle",`
 width: 200px;
 color: inherit;
 `,[_("progress-text",`
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
 `)]),_("progress-content",{position:"relative"}),_("progress-graph",{position:"relative"},[_("progress-graph-circle",[H("svg",{verticalAlign:"bottom"}),_("progress-graph-circle-fill",`
 stroke: var(--n-fill-color);
 transition:
 opacity .3s var(--n-bezier),
 stroke .3s var(--n-bezier),
 stroke-dasharray .3s var(--n-bezier);
 `,[O("empty",{opacity:0})]),_("progress-graph-circle-rail",`
 transition: stroke .3s var(--n-bezier);
 overflow: hidden;
 stroke: var(--n-rail-color);
 `)]),_("progress-graph-line",[O("indicator-inside",[_("progress-graph-line-rail",`
 height: 16px;
 line-height: 16px;
 border-radius: 10px;
 `,[_("progress-graph-line-fill",`
 height: inherit;
 border-radius: 10px;
 `),_("progress-graph-line-indicator",`
 background: #0000;
 white-space: nowrap;
 text-align: right;
 margin-left: 14px;
 margin-right: 14px;
 height: inherit;
 font-size: 12px;
 color: var(--n-text-color-line-inner);
 transition: color .3s var(--n-bezier);
 `)])]),O("indicator-inside-label",`
 height: 16px;
 display: flex;
 align-items: center;
 `,[_("progress-graph-line-rail",`
 flex: 1;
 transition: background-color .3s var(--n-bezier);
 `),_("progress-graph-line-indicator",`
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
 `)]),_("progress-graph-line-rail",`
 position: relative;
 overflow: hidden;
 height: var(--n-rail-height);
 border-radius: 5px;
 background-color: var(--n-rail-color);
 transition: background-color .3s var(--n-bezier);
 `,[_("progress-graph-line-fill",`
 background: var(--n-fill-color);
 position: relative;
 border-radius: 5px;
 height: inherit;
 width: 100%;
 max-width: 0%;
 transition:
 background-color .3s var(--n-bezier),
 max-width .2s var(--n-bezier);
 `,[O("processing",[H("&::after",`
 content: "";
 background-image: var(--n-line-bg-processing);
 animation: progress-processing-animation 2s var(--n-bezier) infinite;
 `)])])])])])]),H("@keyframes progress-processing-animation",`
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
 `)]);const Xe=["aria-valuenow","role"],Ye={...U.props,processing:Boolean,type:{type:String,default:"line"},gapDegree:Number,gapOffsetDegree:Number,status:{type:String,default:"default"},railColor:[String,Array],railStyle:[String,Array],color:[String,Array,Object],viewBoxWidth:{type:Number,default:100},strokeWidth:{type:Number,default:7},percentage:[Number,Array],unit:{type:String,default:"%"},showIndicator:{type:Boolean,default:!0},indicatorPosition:{type:String,default:"outside"},indicatorPlacement:{type:String,default:"outside"},indicatorTextColor:String,circleGap:{type:Number,default:1},height:Number,borderRadius:[String,Number],fillBorderRadius:[String,Number],offsetDegree:Number};var Q=E({name:"Progress",props:Ye,setup(e){const k=B(()=>e.indicatorPlacement||e.indicatorPosition),g=B(()=>{if(e.gapDegree||e.gapDegree===0)return e.gapDegree;if(e.type==="dashboard")return 75}),{mergedClsPrefixRef:b,inlineThemeDisabled:h}=ge(e),d=U("Progress","-progress",Ee,ve,e,b),c=B(()=>{const{status:$}=e,{common:{cubicBezierEaseInOut:x},self:{fontSize:C,fontSizeCircle:S,railColor:o,railHeight:r,iconSizeCircle:i,iconSizeLine:u,textColorCircle:D,textColorLineInner:W,textColorLineOuter:R,lineBgProcessing:I,fontWeightCircle:T,[ee("iconColor",$)]:L,[ee("fillColor",$)]:X}}=d.value;return{"--n-bezier":x,"--n-fill-color":X,"--n-font-size":C,"--n-font-size-circle":S,"--n-font-weight-circle":T,"--n-icon-color":L,"--n-icon-size-circle":i,"--n-icon-size-line":u,"--n-line-bg-processing":I,"--n-rail-color":o,"--n-rail-height":r,"--n-text-color-circle":D,"--n-text-color-line-inner":W,"--n-text-color-line-outer":R}}),y=h?ye("progress",B(()=>e.status[0]),c,e):void 0;return{mergedClsPrefix:b,mergedIndicatorPlacement:k,gapDeg:g,cssVars:h?void 0:c,themeClass:y?.themeClass,onRender:y?.onRender}},render(){const{type:e,cssVars:k,indicatorTextColor:g,showIndicator:b,status:h,railColor:d,railStyle:c,color:y,percentage:$,viewBoxWidth:x,strokeWidth:C,mergedIndicatorPlacement:S,unit:o,borderRadius:r,fillBorderRadius:i,height:u,processing:D,circleGap:W,mergedClsPrefix:R,gapDeg:I,gapOffsetDegree:T,themeClass:L,$slots:X,onRender:fe}=this;return fe?.(),s(),p("div",{class:v([L,`${R}-progress`,`${R}-progress--${e}`,`${R}-progress--${h}`]),style:N(k),"aria-valuemax":100,"aria-valuemin":0,"aria-valuenow":$,role:e==="circle"||e==="line"||e==="dashboard"?"progressbar":"none"},[e==="circle"||e==="dashboard"?(s(),m(Ae,{key:0,clsPrefix:R,status:h,showIndicator:b,indicatorTextColor:g,railColor:d,fillColor:y,railStyle:c,offsetDegree:this.offsetDegree,percentage:$,viewBoxWidth:x,strokeWidth:C,gapDegree:I===void 0?e==="dashboard"?75:0:I,gapOffsetDegree:T,unit:o},F(X),1032,["clsPrefix","status","showIndicator","indicatorTextColor","railColor","fillColor","railStyle","offsetDegree","percentage","viewBoxWidth","strokeWidth","gapDegree","gapOffsetDegree","unit"])):(s(),p(A,{key:1},[e==="line"?(s(),m(qe,{key:0,clsPrefix:R,status:h,showIndicator:b,indicatorTextColor:g,railColor:d,fillColor:y,railStyle:c,percentage:$,processing:D,indicatorPlacement:S,unit:o,fillBorderRadius:i,railBorderRadius:r,height:u},F(X),1032,["clsPrefix","status","showIndicator","indicatorTextColor","railColor","fillColor","railStyle","percentage","processing","indicatorPlacement","unit","fillBorderRadius","railBorderRadius","height"])):(s(),p(A,{key:1},[e==="multiple-circle"?(s(),m(He,{key:0,clsPrefix:R,strokeWidth:C,railColor:d,fillColor:y,railStyle:c,viewBoxWidth:x,percentage:$,showIndicator:b,circleGap:W},F(X),1032,["clsPrefix","strokeWidth","railColor","fillColor","railStyle","viewBoxWidth","percentage","showIndicator","circleGap"])):z(()=>null)],64))],64))],14,Xe)}});function Ue(e){const{heightSmall:k,heightMedium:g,heightLarge:b,borderRadius:h}=e;return{color:"#eee",colorEnd:"#ddd",borderRadius:h,heightSmall:k,heightMedium:g,heightLarge:b}}const Fe={common:be,self:Ue};var Ze=H([_("skeleton",`
 height: 1em;
 width: 100%;
 transition:
 --n-color-start .3s var(--n-bezier),
 --n-color-end .3s var(--n-bezier),
 background-color .3s var(--n-bezier);
 animation: 2s skeleton-loading infinite cubic-bezier(0.36, 0, 0.64, 1);
 background-color: var(--n-color-start);
 `),H("@keyframes skeleton-loading",`
 0% {
 background: var(--n-color-start);
 }
 40% {
 background: var(--n-color-end);
 }
 80% {
 background: var(--n-color-start);
 }
 100% {
 background: var(--n-color-start);
 }
 `)]);const Je={...U.props,text:Boolean,round:Boolean,circle:Boolean,height:[String,Number],width:[String,Number],size:String,repeat:{type:Number,default:1},animated:{type:Boolean,default:!0},sharp:{type:Boolean,default:!0}};var le=E({name:"Skeleton",inheritAttrs:!1,props:Je,setup(e){ze();const{mergedClsPrefixRef:k,mergedComponentPropsRef:g}=ge(e),b=B(()=>e.size||g?.value?.Skeleton?.size),h=U("Skeleton","-skeleton",Ze,Fe,e,k);return{mergedClsPrefix:k,style:B(()=>{const d=h.value,{common:{cubicBezierEaseInOut:c}}=d,y=d.self,{color:$,colorEnd:x,borderRadius:C}=y;let S;const{circle:o,sharp:r,round:i,width:u,height:D,text:W,animated:R}=e,I=b.value;I!==void 0&&(S=y[ee("height",I)]);const T=o?u??D??S:u,L=(o?u??D:D)??S;return{display:W?"inline-block":"",verticalAlign:W?"-0.125em":"",borderRadius:o?"50%":i?"4096px":r?"":C,width:typeof T=="number"?re(T):T,height:typeof L=="number"?re(L):L,animation:R?"":"none","--n-bezier":c,"--n-color-start":$,"--n-color-end":x}})}},render(){const{repeat:e,style:k,mergedClsPrefix:g,$attrs:b}=this,h=ke("div",_e({class:`${g}-skeleton`,style:k},b));return e>1?(s(),p(A,{key:1},[z(()=>xe(e,null).map(d=>[h,`
`]))],64)):h}});const Ke={class:"dash"},Qe={class:"page-head"},et={class:"page-actions"},tt={key:0,class:"dash-alert"},rt={class:"kpi-row"},it={class:"kpi-value num"},st={class:"kpi-unit"},ot={class:"kpi-sub"},at={class:"dash-split"},lt={class:"dash-main"},nt={class:"section-title"},dt={class:"card-foot"},ct={class:"section-title"},ut={key:1,class:"node-grid"},pt={class:"node-avatar","aria-hidden":"true"},gt={class:"node-head"},ft={class:"node-metrics"},ht={class:"node-metric"},mt={class:"node-metric"},yt={class:"node-metric"},vt={class:"dash-aside"},bt=E({__name:"DashboardPage",setup(e){const k=[],g=Ce(),b=B(()=>g.servers),h=B(()=>b.value.filter(o=>o.status==="ready").length),d=B(()=>b.value.length),c=B(()=>b.value.filter(o=>o.status==="offline"||o.status==="error"));function y(o){const r=o.replace(/[^a-zA-Z0-9]+/g," ").trim().split(/\s+/);return r.length===1?o.slice(0,2).toUpperCase():(r[0][0]+r[1][0]).toUpperCase()}function $(o){const r=[`${o.ip}:${o.port}`];return o.os&&r.push(o.os),o.docker_version&&r.push(`Docker ${o.docker_version}`),r.join(" · ")}function x(o){if(o==null)return 0;const r=o<=1?o*100:o;return Math.round(Math.min(Math.max(r,0),100))}function C(o){if(o==null)return"default";const r=x(o);return r>=80?"error":r>=60?"warning":"success"}function S(o){return o==null?"—":`${x(o)}%`}return $e(()=>{g.fetchServers().catch(()=>{}),g.pollServers()}),Se(()=>{g.stopPolling()}),(o,r)=>(s(),p("div",Ke,[n("div",Qe,[r[2]||(r[2]=n("div",null,[n("p",{class:"eyebrow"},"Overview"),n("h1",null,"Dashboard"),n("p",{class:"page-desc"}," One control plane for every node, application, and database. Widgets without a backend show an explicit empty state until their phase lands. ")],-1)),n("div",et,[l(t(Z),{to:{name:"servers"},custom:""},{default:a(({navigate:i})=>[l(t(K),{quaternary:"",onClick:i},{default:a(()=>[...r[0]||(r[0]=[f("View servers",-1)])]),_:1},8,["onClick"])]),_:1}),l(t(Z),{to:{name:"servers",query:{add:"1"}},custom:""},{default:a(({navigate:i})=>[l(t(K),{type:"primary",onClick:i},{default:a(()=>[...r[1]||(r[1]=[f("Add server",-1)])]),_:1},8,["onClick"])]),_:1})])]),t(g).error?(s(),p("div",tt,[l(t(te),{type:"error","show-icon":!0},{default:a(()=>[f(P(t(g).error),1)]),_:1})])):M("",!0),n("div",rt,[l(t(q),{class:"kpi kpi--live",title:"Servers ready",size:"small"},{default:a(()=>[t(g).loading&&d.value===0?(s(),m(t(le),{key:0,text:"",repeat:2})):(s(),p(A,{key:1},[n("p",it,[f(P(h.value),1),n("span",st,"/"+P(d.value),1)]),n("p",ot,[d.value>0?(s(),m(ie,{key:0,status:c.value.length>0?"offline":"ready"},null,8,["status"])):(s(),m(t(w),{key:1,depth:"3"},{default:a(()=>[...r[3]||(r[3]=[f("No servers yet — add one to begin.",-1)])]),_:1})),c.value.length>0?(s(),m(t(w),{key:2,depth:"3"},{default:a(()=>[f(P(c.value.map(i=>i.name).join(", "))+" unreachable ",1)]),_:1})):M("",!0)])],64))]),_:1}),l(t(q),{class:"kpi",title:"Running applications",size:"small"},{default:a(()=>[l(t(j),{size:"small",description:"No applications yet — ships in Phase 4"})]),_:1}),l(t(q),{class:"kpi",title:"Deploys in 24h",size:"small"},{default:a(()=>[l(t(j),{size:"small",description:"No deploys yet — ships in Phase 4"})]),_:1}),l(t(q),{class:"kpi",title:"SSL certificates",size:"small"},{default:a(()=>[l(t(j),{size:"small",description:"No certificates yet — ships in Phase 6"})]),_:1})]),n("div",at,[n("div",lt,[n("div",nt,[r[5]||(r[5]=n("h2",null,"Recent deploys",-1)),l(t(w),{depth:"3",class:"mono meta"},{default:a(()=>[...r[4]||(r[4]=[f("source: deployments · realtime via Redis",-1)])]),_:1})]),l(t(q),{size:"small"},{default:a(()=>[k.length===0?(s(),m(t(j),{key:0,description:"No deployments yet — ships in Phase 4"},{extra:a(()=>[l(t(w),{depth:"3"},{default:a(()=>[...r[6]||(r[6]=[f(" Push an application to see build history, durations, and statuses here. ",-1)])]),_:1})]),_:1})):M("",!0),l(t(V),{vertical:"",size:12},{default:a(()=>[n("div",dt,[l(t(w),{depth:"3"},{default:a(()=>[...r[7]||(r[7]=[f("Queue: no data yet",-1)])]),_:1}),l(t(w),{depth:"3"},{default:a(()=>[...r[8]||(r[8]=[f("Build pipeline ships in Phase 4",-1)])]),_:1})])]),_:1})]),_:1}),n("div",ct,[r[10]||(r[10]=n("h2",null,"Server health",-1)),l(t(w),{depth:"3",class:"mono meta"},{default:a(()=>[...r[9]||(r[9]=[f(" heartbeat every 10s over gRPC server-authenticated TLS ",-1)])]),_:1})]),!t(g).loading&&d.value===0?(s(),m(t(j),{key:0,description:"No servers yet"},{extra:a(()=>[l(t(V),{vertical:"",size:8,align:"center"},{default:a(()=>[l(t(w),{depth:"3"},{default:a(()=>[...r[11]||(r[11]=[f(" Add your first node to see CPU, RAM, and disk health here. ",-1)])]),_:1}),l(t(Z),{to:{name:"servers",query:{add:"1"}},custom:""},{default:a(({navigate:i})=>[l(t(K),{type:"primary",size:"small",onClick:i},{default:a(()=>[...r[12]||(r[12]=[f(" Add server ",-1)])]),_:1},8,["onClick"])]),_:1})]),_:1})]),_:1})):(s(),p("div",ut,[t(g).loading&&d.value===0?(s(),m(t(le),{key:0,text:"",repeat:3})):M("",!0),(s(!0),p(A,null,J(b.value,i=>(s(),m(t(q),{key:i.id,size:"small",class:"node-card"},{default:a(()=>[l(t(V),{vertical:"",size:12},{default:a(()=>[l(t(V),{align:"center",size:12,wrap:!1},{default:a(()=>[n("div",pt,P(y(i.name)),1),n("div",gt,[l(t(w),{strong:""},{default:a(()=>[f(P(i.name),1)]),_:2},1024),l(t(w),{depth:"3",class:"node-sub"},{default:a(()=>[f(P($(i)),1)]),_:2},1024)]),l(ie,{status:i.status},null,8,["status"])]),_:2},1024),n("div",ft,[n("div",ht,[l(t(w),{depth:"3",class:"metric-label"},{default:a(()=>[...r[13]||(r[13]=[f("CPU",-1)])]),_:1}),l(t(w),{class:"num metric-val"},{default:a(()=>[f(P(S(i.cpu_usage)),1)]),_:2},1024),l(t(Q),{type:"line",percentage:x(i.cpu_usage),"show-indicator":!1,status:C(i.cpu_usage)},null,8,["percentage","status"])]),n("div",mt,[l(t(w),{depth:"3",class:"metric-label"},{default:a(()=>[...r[14]||(r[14]=[f("RAM",-1)])]),_:1}),l(t(w),{class:"num metric-val"},{default:a(()=>[f(P(S(i.mem_usage)),1)]),_:2},1024),l(t(Q),{type:"line",percentage:x(i.mem_usage),"show-indicator":!1,status:C(i.mem_usage)},null,8,["percentage","status"])]),n("div",yt,[l(t(w),{depth:"3",class:"metric-label"},{default:a(()=>[...r[15]||(r[15]=[f("Disk",-1)])]),_:1}),l(t(w),{class:"num metric-val"},{default:a(()=>[f(P(S(i.disk_usage)),1)]),_:2},1024),l(t(Q),{type:"line",percentage:x(i.disk_usage),"show-indicator":!1,status:C(i.disk_usage)},null,8,["percentage","status"])])]),l(t(V),{align:"center",size:8},{default:a(()=>[i.container_count!==null?(s(),m(t(Y),{key:0,size:"small",bordered:!1},{default:a(()=>[f(P(i.container_count)+" containers ",1)]),_:2},1024)):M("",!0),i.arch?(s(),m(t(Y),{key:1,size:"small",bordered:!1},{default:a(()=>[f(P(i.arch),1)]),_:2},1024)):M("",!0),i.ssh_user?(s(),m(t(Y),{key:2,size:"small",bordered:!1},{default:a(()=>[f(P(i.ssh_user),1)]),_:2},1024)):M("",!0)]),_:2},1024)]),_:2},1024)]),_:2},1024))),128))]))]),n("aside",vt,[l(t(q),{size:"small",title:"Heartbeat",class:"aside-card"},{"header-extra":a(()=>[l(t(Y),{size:"small",type:"success",bordered:!1},{default:a(()=>[...r[16]||(r[16]=[n("span",{class:"pulse-dot","aria-hidden":"true"},null,-1),f("live ",-1)])]),_:1})]),footer:a(()=>[l(t(w),{depth:"3",class:"mono meta"},{default:a(()=>[...r[17]||(r[17]=[f("10s cycle · Heartbeat(stream) in agent.v1",-1)])]),_:1})]),default:a(()=>[d.value===0?(s(),m(t(j),{key:0,size:"small",description:"No heartbeats yet — add a server"})):(s(),m(t(V),{key:1,vertical:"",size:8},{default:a(()=>[(s(!0),p(A,null,J(b.value,i=>(s(),p("div",{key:i.id,class:"heartbeat-row"},[l(t(w),{depth:"2"},{default:a(()=>[f(P(i.name),1)]),_:2},1024),l(t(w),{depth:"3",class:"mono"},{default:a(()=>[f(P(t(se)(i.last_seen)),1)]),_:2},1024)]))),128))]),_:1}))]),_:1}),l(t(q),{size:"small",title:"Control-plane components",class:"aside-card"},{default:a(()=>[l(t(j),{size:"small",description:"No component telemetry yet"},{extra:a(()=>[l(t(w),{depth:"3"},{default:a(()=>[...r[18]||(r[18]=[f(" Postgres, Redis, and gateway health ships with a status endpoint in a later phase. ",-1)])]),_:1})]),_:1})]),_:1}),l(t(q),{size:"small",title:"Team activity",class:"aside-card"},{default:a(()=>[l(t(j),{size:"small",description:"No team activity yet — ships in Phase 8"})]),_:1}),l(t(q),{size:"small",title:"Alerts",class:"aside-card"},{default:a(()=>[c.value.length>0?(s(),m(t(V),{key:0,vertical:"",size:8},{default:a(()=>[(s(!0),p(A,null,J(c.value,i=>(s(),m(t(te),{key:i.id,type:"error","show-icon":!0,title:`${i.name} unreachable`},{default:a(()=>[f(" Agent heartbeat lost. Last seen "+P(t(se)(i.last_seen))+". ",1)]),_:2},1032,["title"]))),128))]),_:1})):(s(),m(t(j),{key:1,size:"small",description:"No alerts — all nodes healthy"}))]),_:1})])])]))}}),Dt=we(bt,[["__scopeId","data-v-18738b0f"]]);export{Dt as default};
