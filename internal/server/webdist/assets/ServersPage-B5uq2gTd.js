import{A as ve}from"./Alert-DRSmLNGq.js";import{d as J,o as t,c as p,a as I,E as b,G as $,H as be,I as kt,J as wt,K as Ct,L as St,M as $t,N as O,O as f,P as ge,l as C,Q as Se,S as zt,q as A,T as ue,z as Ye,U as Ze,y as T,V as le,W as Pt,X as We,Y as It,Z as ye,_ as pe,$ as qe,a0 as Ue,a1 as H,F,a2 as Je,a3 as Qe,a4 as et,a5 as tt,a6 as _t,a7 as Y,a8 as Me,a9 as Rt,aa as rt,ab as L,ac as we,ad as Ve,ae as je,af as Bt,ag as Nt,ah as Dt,ai as Mt,e as c,aj as Vt,ak as Tt,al as At,am as Ot,u as a,an as Ut,w as d,k as B,t as oe,m as ae,n as Ft,r as Wt,B as te,s as qt,g as jt,A as Et,C as Ht,ao as ce}from"./index-C7ntX97-.js";import{s as Kt,r as Lt,C as Gt,R as Xt,D as Yt}from"./DataTable-DwuJr8Ta.js";import{u as it}from"./use-message-CjT75cX4.js";import{g as Zt,S as X,t as G}from"./text-DPRXPgUg.js";import{A as Jt,_ as nt,f as Ee,r as Qt,P as er}from"./format-_x0teFeR.js";import{u as tr,f as fe}from"./format-length-x6HLSQPw.js";import{T as rr,c as ir,d as Ce}from"./servers-CVJJGNoa.js";import{I as se}from"./Input-8pAf-74X.js";import{F as nr,a as ne}from"./FormItem-B9lKVqQT.js";import{u as or}from"./use-locale-seOFRm4U.js";import{u as ot}from"./servers-DBhI_gU0.js";import{_ as ar}from"./_plugin-vue_export-helper-DlAUqK2U.js";import"./Popover-Dnfl-wZH.js";import"./Dropdown-Bk2mjSjn.js";import"./Empty-BtXaOizh.js";import"./CheckboxGroup-BPIIgvLu.js";import"./Tooltip-B9VcnjlU.js";const sr=["value","name","checked","disabled","onChange","onFocus","onBlur"];var He=J({name:"RadioButton",props:Lt,setup:Kt,render(){const{mergedClsPrefix:e}=this;return t(),p("label",{class:b([`${e}-radio-button`,this.mergedDisabled&&`${e}-radio-button--disabled`,this.renderSafeChecked&&`${e}-radio-button--checked`,this.focus&&[`${e}-radio-button--focus`]])},[I("input",{ref:"inputRef",type:"radio",class:b(`${e}-radio-input`),value:this.value,name:this.mergedName,checked:this.renderSafeChecked,disabled:this.mergedDisabled,onChange:this.handleRadioInputChange,onFocus:this.handleRadioInputFocus,onBlur:this.handleRadioInputBlur},null,42,sr),I("div",{class:b(`${e}-radio-button__state-border`)},null,2),$(()=>be(this.$slots.default,s=>!s&&!this.label?null:(t(),p("div",{ref:"labelRef",class:b(`${e}-radio__label`)},[$(()=>s||this.label)],2))))],2)}}),lr=J({name:"Remove",render(){return(()=>{const e=kt("a77472467b8adb0a");return e[0]||(e[0]=I("svg",{xmlns:"http://www.w3.org/2000/svg",viewBox:"0 0 512 512"},[I("line",{x1:"400",y1:"256",x2:"112",y2:"256",style:`
        fill: none;
        stroke: currentColor;
        stroke-linecap: round;
        stroke-linejoin: round;
        stroke-width: 32px;
      `})],-1))})()}});function dr(e){const{textColorDisabled:s}=e;return{iconColorDisabled:s}}const ur=wt({name:"InputNumber",common:$t,peers:{Button:St,Input:Ct},self:dr});var cr=O([f("input-number-suffix",`
 display: inline-block;
 margin-right: 10px;
 `),f("input-number-prefix",`
 display: inline-block;
 margin-left: 10px;
 `)]);function fr(e){return e==null||typeof e=="string"&&e.trim()===""?null:Number(e)}function pr(e){return e.includes(".")&&(/^(-)?\d+.*(\.|0)$/.test(e)||/^-?\d*$/.test(e))||e==="-"||e==="-0"}function Te(e){return e==null?!0:!Number.isNaN(e)}function Ke(e,s){return typeof e!="number"?"":s===void 0?String(e):e.toFixed(s)}function Ae(e){if(e===null)return null;if(typeof e=="number")return e;{const s=Number(e);return Number.isNaN(s)?null:s}}const Le=800,Ge=100,gr={...ge.props,autofocus:Boolean,loading:{type:Boolean,default:void 0},placeholder:String,defaultValue:{type:Number,default:null},value:Number,step:{type:[Number,String],default:1},min:[Number,String],max:[Number,String],size:String,disabled:{type:Boolean,default:void 0},validator:Function,bordered:{type:Boolean,default:void 0},showButton:{type:Boolean,default:!0},buttonPlacement:{type:String,default:"right"},inputProps:Object,readonly:Boolean,clearable:Boolean,keyboard:{type:Object,default:{}},updateValueOnInput:{type:Boolean,default:!0},round:{type:Boolean,default:void 0},parse:Function,format:Function,precision:Number,status:String,"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],onFocus:[Function,Array],onBlur:[Function,Array],onClear:[Function,Array],onChange:[Function,Array]};var hr=J({name:"InputNumber",props:gr,slots:Object,setup(e){const{mergedBorderedRef:s,mergedClsPrefixRef:m,mergedRtlRef:S,mergedComponentPropsRef:v}=Se(e),u=ge("InputNumber","-input-number",cr,ur,e,m),{localeRef:k}=or("InputNumber"),y=zt(e,{mergedSize:o=>{const{size:w}=e;if(w)return w;const{mergedSize:D}=o||{};if(D?.value)return D.value;const E=v?.value?.InputNumber?.size;return E||"medium"}}),{mergedSizeRef:z,mergedDisabledRef:_,mergedStatusRef:P}=y,r=A(null),i=A(null),g=A(null),h=A(e.defaultValue),x=Ue(e,"value"),l=tr(x,h),V=A(""),N=o=>{const w=String(o).split(".")[1];return w?w.length:0},U=o=>{const w=[e.min,e.max,e.step,o].map(D=>D===void 0?0:N(D));return Math.max(...w)},K=ue(()=>{const{placeholder:o}=e;return o!==void 0?o:k.value.placeholder}),W=ue(()=>{const o=Ae(e.step);return o!==null?o===0?1:Math.abs(o):1}),q=ue(()=>{const o=Ae(e.min);return o!==null?o:null}),re=ue(()=>{const o=Ae(e.max);return o!==null?o:null}),Z=()=>{const{value:o}=l;if(Te(o)){const{format:w,precision:D}=e;w?V.value=w(o):o===null||D===void 0||N(o)>D?V.value=Ke(o,void 0):V.value=Ke(o,D)}else V.value=String(o)};Z();const ee=o=>{const{value:w}=l;if(o===w){Z();return}const{"onUpdate:value":D,onUpdateValue:E,onChange:Q}=e,{nTriggerFormInput:de,nTriggerFormChange:Re}=y;Q&&le(Q,o),E&&le(E,o),D&&le(D,o),h.value=o,de(),Re()},j=({offset:o,doUpdateIfValid:w,fixPrecision:D,isInputing:E})=>{const{value:Q}=V;if(E&&pr(Q))return!1;const de=(e.parse||fr)(Q);if(de===null)return w&&ee(null),null;if(Te(de)){const Re=N(de),{precision:Be}=e;if(Be!==void 0&&Be<Re&&!D)return!1;let ie=Number.parseFloat((de+o).toFixed(Be??U(de)));if(Te(ie)){const{value:Ne}=re,{value:De}=q;if(Ne!==null&&ie>Ne){if(!w||E)return!1;ie=Ne}if(De!==null&&ie<De){if(!w||E)return!1;ie=De}return e.validator&&!e.validator(ie)?!1:(w&&ee(ie),ie)}}return!1},M=ue(()=>j({offset:0,doUpdateIfValid:!1,isInputing:!1,fixPrecision:!1})===!1),n=ue(()=>{const{value:o}=l;if(e.validator&&o===null)return!1;const{value:w}=W;return j({offset:-w,doUpdateIfValid:!1,isInputing:!1,fixPrecision:!1})!==!1}),R=ue(()=>{const{value:o}=l;if(e.validator&&o===null)return!1;const{value:w}=W;return j({offset:+w,doUpdateIfValid:!1,isInputing:!1,fixPrecision:!1})!==!1});function $e(o){const{onFocus:w}=e,{nTriggerFormFocus:D}=y;w&&le(w,o),D()}function st(o){if(o.target===r.value?.wrapperElRef)return;const w=j({offset:0,doUpdateIfValid:!0,isInputing:!1,fixPrecision:!0});if(w!==!1){const Q=r.value?.inputElRef;Q&&(Q.value=String(w||"")),l.value===w&&Z()}else Z();const{onBlur:D}=e,{nTriggerFormBlur:E}=y;D&&le(D,o),E(),Pt(()=>{Z()})}function lt(o){const{onClear:w}=e;w&&le(w,o)}function ze(){const{value:o}=R;if(!o){_e();return}const{value:w}=l;if(w===null)e.validator||ee(Fe());else{const{value:D}=W;j({offset:D,doUpdateIfValid:!0,isInputing:!1,fixPrecision:!0})}}function Pe(){const{value:o}=n;if(!o){Ie();return}const{value:w}=l;if(w===null)e.validator||ee(Fe());else{const{value:D}=W;j({offset:-D,doUpdateIfValid:!0,isInputing:!1,fixPrecision:!0})}}const dt=$e,ut=st;function Fe(){if(e.validator)return null;const{value:o}=q,{value:w}=re;return o!==null?Math.max(0,o):w!==null?Math.min(0,w):0}function ct(o){lt(o),ee(null)}function ft(o){g.value?.$el.contains(o.target)&&o.preventDefault(),i.value?.$el.contains(o.target)&&o.preventDefault(),r.value?.activate()}let he=null,me=null,xe=null;function Ie(){xe&&(window.clearTimeout(xe),xe=null),he&&(window.clearInterval(he),he=null)}let ke=null;function _e(){ke&&(window.clearTimeout(ke),ke=null),me&&(window.clearInterval(me),me=null)}function pt(){Ie(),xe=window.setTimeout(()=>{he=window.setInterval(()=>{Pe()},Ge)},Le),We("mouseup",document,Ie,{once:!0})}function gt(){_e(),ke=window.setTimeout(()=>{me=window.setInterval(()=>{ze()},Ge)},Le),We("mouseup",document,_e,{once:!0})}const ht=()=>{me||ze()},mt=()=>{he||Pe()};function vt(o){if(o.key==="Enter"){if(o.target===r.value?.wrapperElRef)return;j({offset:0,doUpdateIfValid:!0,isInputing:!1,fixPrecision:!0})!==!1&&r.value?.deactivate()}else if(o.key==="ArrowUp"){if(!R.value||e.keyboard.ArrowUp===!1)return;o.preventDefault(),j({offset:0,doUpdateIfValid:!0,isInputing:!1,fixPrecision:!0})!==!1&&ze()}else if(o.key==="ArrowDown"){if(!n.value||e.keyboard.ArrowDown===!1)return;o.preventDefault(),j({offset:0,doUpdateIfValid:!0,isInputing:!1,fixPrecision:!0})!==!1&&Pe()}}function yt(o){V.value=o,e.updateValueOnInput&&!e.format&&!e.parse&&e.precision===void 0&&j({offset:0,doUpdateIfValid:!0,isInputing:!0,fixPrecision:!1})}Ye(l,()=>{Z()});const bt={focus:()=>r.value?.focus(),blur:()=>r.value?.blur(),select:()=>r.value?.select()},xt=Ze("InputNumber",S,m);return{...bt,rtlEnabled:xt,inputInstRef:r,minusButtonInstRef:i,addButtonInstRef:g,mergedClsPrefix:m,mergedBordered:s,uncontrolledValue:h,mergedValue:l,mergedPlaceholder:K,displayedValueInvalid:M,mergedSize:z,mergedDisabled:_,displayedValue:V,addable:R,minusable:n,mergedStatus:P,handleFocus:dt,handleBlur:ut,handleClear:ct,handleMouseDown:ft,handleAddClick:ht,handleMinusClick:mt,handleAddMousedown:gt,handleMinusMousedown:pt,handleKeyDown:vt,handleUpdateDisplayedValue:yt,mergedTheme:u,inputThemeOverrides:{paddingSmall:"0 8px 0 10px",paddingMedium:"0 8px 0 12px",paddingLarge:"0 8px 0 14px"},buttonThemeOverrides:T(()=>{const{self:{iconColorDisabled:o}}=u.value,[w,D,E,Q]=It(o);return{textColorTextDisabled:`rgb(${w}, ${D}, ${E})`,opacityDisabled:`${Q}`}})}},render(){const{mergedClsPrefix:e,$slots:s}=this,m=()=>(t(),C(qe,{text:!0,disabled:!this.minusable||this.mergedDisabled||this.readonly,focusable:!1,theme:this.mergedTheme.peers.Button,themeOverrides:this.mergedTheme.peerOverrides.Button,builtinThemeOverrides:this.buttonThemeOverrides,onClick:this.handleMinusClick,onMousedown:this.handleMinusMousedown,ref:"minusButtonInstRef"},{icon:()=>ye(s["minus-icon"],()=>[(t(),C(pe,{clsPrefix:e},{default:()=>(t(),C(lr))},1032,["clsPrefix"]))])},1032,["disabled","theme","themeOverrides","builtinThemeOverrides","onClick","onMousedown"])),S=()=>(t(),C(qe,{text:!0,disabled:!this.addable||this.mergedDisabled||this.readonly,focusable:!1,theme:this.mergedTheme.peers.Button,themeOverrides:this.mergedTheme.peerOverrides.Button,builtinThemeOverrides:this.buttonThemeOverrides,onClick:this.handleAddClick,onMousedown:this.handleAddMousedown,ref:"addButtonInstRef"},{icon:()=>ye(s["add-icon"],()=>[(t(),C(pe,{clsPrefix:e},{default:()=>(t(),C(Jt))},1032,["clsPrefix"]))])},1032,["disabled","theme","themeOverrides","builtinThemeOverrides","onClick","onMousedown"]));return t(),p("div",{class:b([`${e}-input-number`,this.rtlEnabled&&`${e}-input-number--rtl`])},[(t(),C(se,{ref:"inputInstRef",autofocus:this.autofocus,status:this.mergedStatus,bordered:this.mergedBordered,loading:this.loading,value:this.displayedValue,onUpdateValue:this.handleUpdateDisplayedValue,theme:this.mergedTheme.peers.Input,themeOverrides:this.mergedTheme.peerOverrides.Input,builtinThemeOverrides:this.inputThemeOverrides,size:this.mergedSize,placeholder:this.mergedPlaceholder,disabled:this.mergedDisabled,readonly:this.readonly,round:this.round,textDecoration:this.displayedValueInvalid?"line-through":void 0,onFocus:this.handleFocus,onBlur:this.handleBlur,onKeydown:this.handleKeyDown,onMousedown:this.handleMouseDown,onClear:this.handleClear,clearable:this.clearable,inputProps:this.inputProps,internalLoadingBeforeSuffix:!0},{prefix:()=>this.showButton&&this.buttonPlacement==="both"?[m(),be(s.prefix,v=>v?(t(),p("span",{key:1,class:b(`${e}-input-number-prefix`)},[$(()=>v)],2)):null)]:s.prefix?.(),suffix:()=>this.showButton?[be(s.suffix,v=>v?(t(),p("span",{key:2,class:b(`${e}-input-number-suffix`)},[$(()=>v)],2)):null),this.buttonPlacement==="right"?m():null,S()]:s.suffix?.()},1032,["autofocus","status","bordered","loading","value","onUpdateValue","theme","themeOverrides","builtinThemeOverrides","size","placeholder","disabled","readonly","round","textDecoration","onFocus","onBlur","onKeydown","onMousedown","onClear","clearable","inputProps"]))],2)}});const mr=["id"],vr=["stop-color"],yr=["stop-color"],br=["viewBox"],xr=["d","stroke-width"],kr=["d","stroke-width"],wr={success:(t(),C(tt)),error:(t(),C(et)),warning:(t(),C(Qe)),info:(t(),C(Je))};var Cr=J({name:"ProgressCircle",props:{clsPrefix:{type:String,required:!0},status:{type:String,required:!0},strokeWidth:{type:Number,required:!0},fillColor:[String,Object],railColor:String,railStyle:[String,Object],percentage:{type:Number,default:0},offsetDegree:{type:Number,default:0},showIndicator:{type:Boolean,required:!0},indicatorTextColor:String,unit:String,viewBoxWidth:{type:Number,required:!0},gapDegree:{type:Number,required:!0},gapOffsetDegree:{type:Number,default:0}},setup(e,{slots:s}){const m=T(()=>{const u="gradient",{fillColor:k}=e;return typeof k=="object"?`${u}-${_t(JSON.stringify(k))}`:u});function S(u,k,y,z){const{gapDegree:_,viewBoxWidth:P,strokeWidth:r}=e,i=50,g=0,h=i,x=0,l=100,V=50+r/2,N=`M ${V},${V} m ${g},${h}
      a ${i},${i} 0 1 1 ${x},-100
      a ${i},${i} 0 1 1 0,${l}`,U=Math.PI*2*i;return{pathString:N,pathStyle:{stroke:z==="rail"?y:typeof e.fillColor=="object"?`url(#${m.value})`:y,strokeDasharray:`${Math.min(u,100)/100*(U-_)}px ${P*8}px`,strokeDashoffset:`-${_/2}px`,transformOrigin:k?"center":void 0,transform:k?`rotate(${k}deg)`:void 0}}}const v=()=>{const u=typeof e.fillColor=="object",k=u?e.fillColor.stops[0]:"",y=u?e.fillColor.stops[1]:"";return u&&(t(),p("defs",null,[I("linearGradient",{id:m.value,x1:"0%",y1:"100%",x2:"100%",y2:"0%"},[I("stop",{offset:"0%","stop-color":k},null,8,vr),I("stop",{offset:"100%","stop-color":y},null,8,yr)],8,mr)]))};return()=>{const{fillColor:u,railColor:k,strokeWidth:y,offsetDegree:z,status:_,percentage:P,showIndicator:r,indicatorTextColor:i,unit:g,gapOffsetDegree:h,clsPrefix:x}=e,{pathString:l,pathStyle:V}=S(100,0,k,"rail"),{pathString:N,pathStyle:U}=S(P,z,u,"fill"),K=100+y;return t(),p("div",{class:b(`${x}-progress-content`),role:"none"},[I("div",{class:b(`${x}-progress-graph`),"aria-hidden":!0},[I("div",{class:b(`${x}-progress-graph-circle`),style:H({transform:h?`rotate(${h}deg)`:void 0})},[(t(),p("svg",{viewBox:`0 0 ${K} ${K}`},[$(()=>v()),I("g",null,[I("path",{class:b(`${x}-progress-graph-circle-rail`),d:l,"stroke-width":y,"stroke-linecap":"round",fill:"none",style:H(V)},null,14,xr)]),I("g",null,[I("path",{class:b([`${x}-progress-graph-circle-fill`,P===0&&`${x}-progress-graph-circle-fill--empty`]),d:N,"stroke-width":y,"stroke-linecap":"round",fill:"none",style:H(U)},null,14,kr)])],8,br))],6)],2),r?(t(),p("div",{key:0},[s.default?(t(),p("div",{key:0,class:b(`${x}-progress-custom-content`),role:"none"},[$(()=>s.default())],2)):(t(),p(F,{key:1},[_!=="default"?(t(),p("div",{key:0,class:b(`${x}-progress-icon`),"aria-hidden":!0},[(t(),C(pe,{clsPrefix:x},{default:()=>wr[_]},1032,["clsPrefix"]))],2)):(t(),p("div",{key:1,class:b(`${x}-progress-text`),style:H({color:i}),role:"none"},[I("span",{class:b(`${x}-progress-text__percentage`)},[$(()=>P)],2),I("span",{class:b(`${x}-progress-text__unit`)},[$(()=>g)],2)],6))],64))])):$(()=>null)],2)}}});const Sr={success:(t(),C(tt)),error:(t(),C(et)),warning:(t(),C(Qe)),info:(t(),C(Je))};var $r=J({name:"ProgressLine",props:{clsPrefix:{type:String,required:!0},percentage:{type:Number,default:0},railColor:String,railStyle:[String,Object],fillColor:[String,Object],status:{type:String,required:!0},indicatorPlacement:{type:String,required:!0},indicatorTextColor:String,unit:{type:String,default:"%"},processing:{type:Boolean,required:!0},showIndicator:{type:Boolean,required:!0},height:[String,Number],railBorderRadius:[String,Number],fillBorderRadius:[String,Number]},setup(e,{slots:s}){const m=T(()=>fe(e.height)),S=T(()=>typeof e.fillColor=="object"?`linear-gradient(to right, ${e.fillColor?.stops[0]} , ${e.fillColor?.stops[1]})`:e.fillColor),v=T(()=>e.railBorderRadius!==void 0?fe(e.railBorderRadius):e.height!==void 0?fe(e.height,{c:.5}):""),u=T(()=>e.fillBorderRadius!==void 0?fe(e.fillBorderRadius):e.railBorderRadius!==void 0?fe(e.railBorderRadius):e.height!==void 0?fe(e.height,{c:.5}):"");return()=>{const{indicatorPlacement:k,railColor:y,railStyle:z,percentage:_,unit:P,indicatorTextColor:r,status:i,showIndicator:g,processing:h,clsPrefix:x}=e;return t(),p("div",{class:b(`${x}-progress-content`),role:"none"},[I("div",{class:b(`${x}-progress-graph`),"aria-hidden":!0},[I("div",{class:b([`${x}-progress-graph-line`,{[`${x}-progress-graph-line--indicator-${k}`]:!0}])},[I("div",{class:b(`${x}-progress-graph-line-rail`),style:H([{backgroundColor:y,height:m.value,borderRadius:v.value},z])},[I("div",{class:b([`${x}-progress-graph-line-fill`,h&&`${x}-progress-graph-line-fill--processing`]),style:H({maxWidth:`${e.percentage}%`,background:S.value,height:m.value,lineHeight:m.value,borderRadius:u.value})},[k==="inside"?(t(),p("div",{key:0,class:b(`${x}-progress-graph-line-indicator`),style:H({color:r})},[s.default?(t(),p(F,{key:0},[$(()=>s.default())],64)):(t(),p(F,{key:1},[$(()=>`${_}${P}`)],64))],6)):$(()=>null)],6)],6)],2)],2),g&&k==="outside"?(t(),p("div",{key:0},[s.default?(t(),p("div",{key:0,class:b(`${x}-progress-custom-content`),style:H({color:r}),role:"none"},[$(()=>s.default())],6)):(t(),p(F,{key:1},[i==="default"?(t(),p("div",{key:0,role:"none",class:b(`${x}-progress-icon ${x}-progress-icon--as-text`),style:H({color:r})},[$(()=>_),$(()=>P)],6)):(t(),p("div",{key:1,class:b(`${x}-progress-icon`),"aria-hidden":!0},[(t(),C(pe,{clsPrefix:x},{default:()=>Sr[i]},1032,["clsPrefix"]))],2))],64))])):$(()=>null)],2)}}});const zr=["id"],Pr=["stop-color"],Ir=["stop-color"],_r=["d","stroke-width"],Rr=["d","stroke-width"],Br=["viewBox"];function Xe(e,s,m=100){return`m ${m/2} ${m/2-e} a ${e} ${e} 0 1 1 0 ${2*e} a ${e} ${e} 0 1 1 0 -${2*e}`}var Nr=J({name:"ProgressMultipleCircle",props:{clsPrefix:{type:String,required:!0},viewBoxWidth:{type:Number,required:!0},percentage:{type:Array,default:[0]},strokeWidth:{type:Number,required:!0},circleGap:{type:Number,required:!0},showIndicator:{type:Boolean,required:!0},fillColor:{type:Array,default:()=>[]},railColor:{type:Array,default:()=>[]},railStyle:{type:Array,default:()=>[]}},setup(e,{slots:s}){const m=T(()=>e.percentage.map((v,u)=>`${Math.PI*v/100*(e.viewBoxWidth/2-e.strokeWidth/2*(1+2*u)-e.circleGap*u)*2}, ${e.viewBoxWidth*8}`)),S=(v,u)=>{const k=e.fillColor[u],y=typeof k=="object"?k.stops[0]:"",z=typeof k=="object"?k.stops[1]:"";return typeof e.fillColor[u]=="object"&&(t(),p("linearGradient",{id:`gradient-${u}`,x1:"100%",y1:"0%",x2:"0%",y2:"100%"},[I("stop",{offset:"0%","stop-color":y},null,8,Pr),I("stop",{offset:"100%","stop-color":z},null,8,Ir)],8,zr))};return()=>{const{viewBoxWidth:v,strokeWidth:u,circleGap:k,showIndicator:y,fillColor:z,railColor:_,railStyle:P,percentage:r,clsPrefix:i}=e;return t(),p("div",{class:b(`${i}-progress-content`),role:"none"},[I("div",{class:b(`${i}-progress-graph`),"aria-hidden":!0},[I("div",{class:b(`${i}-progress-graph-circle`)},[(t(),p("svg",{viewBox:`0 0 ${v} ${v}`},[I("defs",null,[$(()=>r.map((g,h)=>S(g,h)))]),$(()=>r.map((g,h)=>(t(),p("g",{key:h},[I("path",{class:b(`${i}-progress-graph-circle-rail`),d:Xe(v/2-u/2*(1+2*h)-k*h,u,v),"stroke-width":u,"stroke-linecap":"round",fill:"none",style:H([{strokeDashoffset:0,stroke:_[h]},P[h]])},null,14,_r),I("path",{class:b([`${i}-progress-graph-circle-fill`,g===0&&`${i}-progress-graph-circle-fill--empty`]),d:Xe(v/2-u/2*(1+2*h)-k*h,u,v),"stroke-width":u,"stroke-linecap":"round",fill:"none",style:H({strokeDasharray:m.value[h],strokeDashoffset:0,stroke:typeof z[h]=="object"?`url(#gradient-${h})`:z[h]})},null,14,Rr)]))))],8,Br))],2)],2),y&&s.default?(t(),p("div",{key:0},[I("div",{class:b(`${i}-progress-text`)},[$(()=>s.default())],2)])):$(()=>null)],2)}}}),Dr=O([f("progress",{display:"inline-block"},[f("progress-icon",`
 color: var(--n-icon-color);
 transition: color .3s var(--n-bezier);
 `),Y("line",`
 width: 100%;
 display: block;
 `,[f("progress-content",`
 display: flex;
 align-items: center;
 `,[f("progress-graph",{flex:1})]),f("progress-custom-content",{marginLeft:"14px"}),f("progress-icon",`
 width: 30px;
 padding-left: 14px;
 height: var(--n-icon-size-line);
 line-height: var(--n-icon-size-line);
 font-size: var(--n-icon-size-line);
 `,[Y("as-text",`
 color: var(--n-text-color-line-outer);
 text-align: center;
 width: 40px;
 font-size: var(--n-font-size);
 padding-left: 4px;
 transition: color .3s var(--n-bezier);
 `)])]),Y("circle, dashboard",{width:"120px"},[f("progress-custom-content",`
 position: absolute;
 left: 50%;
 top: 50%;
 transform: translateX(-50%) translateY(-50%);
 display: flex;
 align-items: center;
 justify-content: center;
 `),f("progress-text",`
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
 `),f("progress-icon",`
 position: absolute;
 left: 50%;
 top: 50%;
 transform: translateX(-50%) translateY(-50%);
 display: flex;
 align-items: center;
 color: var(--n-icon-color);
 font-size: var(--n-icon-size-circle);
 `)]),Y("multiple-circle",`
 width: 200px;
 color: inherit;
 `,[f("progress-text",`
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
 `)]),f("progress-content",{position:"relative"}),f("progress-graph",{position:"relative"},[f("progress-graph-circle",[O("svg",{verticalAlign:"bottom"}),f("progress-graph-circle-fill",`
 stroke: var(--n-fill-color);
 transition:
 opacity .3s var(--n-bezier),
 stroke .3s var(--n-bezier),
 stroke-dasharray .3s var(--n-bezier);
 `,[Y("empty",{opacity:0})]),f("progress-graph-circle-rail",`
 transition: stroke .3s var(--n-bezier);
 overflow: hidden;
 stroke: var(--n-rail-color);
 `)]),f("progress-graph-line",[Y("indicator-inside",[f("progress-graph-line-rail",`
 height: 16px;
 line-height: 16px;
 border-radius: 10px;
 `,[f("progress-graph-line-fill",`
 height: inherit;
 border-radius: 10px;
 `),f("progress-graph-line-indicator",`
 background: #0000;
 white-space: nowrap;
 text-align: right;
 margin-left: 14px;
 margin-right: 14px;
 height: inherit;
 font-size: 12px;
 color: var(--n-text-color-line-inner);
 transition: color .3s var(--n-bezier);
 `)])]),Y("indicator-inside-label",`
 height: 16px;
 display: flex;
 align-items: center;
 `,[f("progress-graph-line-rail",`
 flex: 1;
 transition: background-color .3s var(--n-bezier);
 `),f("progress-graph-line-indicator",`
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
 `)]),f("progress-graph-line-rail",`
 position: relative;
 overflow: hidden;
 height: var(--n-rail-height);
 border-radius: 5px;
 background-color: var(--n-rail-color);
 transition: background-color .3s var(--n-bezier);
 `,[f("progress-graph-line-fill",`
 background: var(--n-fill-color);
 position: relative;
 border-radius: 5px;
 height: inherit;
 width: 100%;
 max-width: 0%;
 transition:
 background-color .3s var(--n-bezier),
 max-width .2s var(--n-bezier);
 `,[Y("processing",[O("&::after",`
 content: "";
 background-image: var(--n-line-bg-processing);
 animation: progress-processing-animation 2s var(--n-bezier) infinite;
 `)])])])])])]),O("@keyframes progress-processing-animation",`
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
 `)]);const Mr=["aria-valuenow","role"],Vr={...ge.props,processing:Boolean,type:{type:String,default:"line"},gapDegree:Number,gapOffsetDegree:Number,status:{type:String,default:"default"},railColor:[String,Array],railStyle:[String,Array],color:[String,Array,Object],viewBoxWidth:{type:Number,default:100},strokeWidth:{type:Number,default:7},percentage:[Number,Array],unit:{type:String,default:"%"},showIndicator:{type:Boolean,default:!0},indicatorPosition:{type:String,default:"outside"},indicatorPlacement:{type:String,default:"outside"},indicatorTextColor:String,circleGap:{type:Number,default:1},height:Number,borderRadius:[String,Number],fillBorderRadius:[String,Number],offsetDegree:Number};var Tr=J({name:"Progress",props:Vr,setup(e){const s=T(()=>e.indicatorPlacement||e.indicatorPosition),m=T(()=>{if(e.gapDegree||e.gapDegree===0)return e.gapDegree;if(e.type==="dashboard")return 75}),{mergedClsPrefixRef:S,inlineThemeDisabled:v}=Se(e),u=ge("Progress","-progress",Dr,Rt,e,S),k=T(()=>{const{status:z}=e,{common:{cubicBezierEaseInOut:_},self:{fontSize:P,fontSizeCircle:r,railColor:i,railHeight:g,iconSizeCircle:h,iconSizeLine:x,textColorCircle:l,textColorLineInner:V,textColorLineOuter:N,lineBgProcessing:U,fontWeightCircle:K,[L("iconColor",z)]:W,[L("fillColor",z)]:q}}=u.value;return{"--n-bezier":_,"--n-fill-color":q,"--n-font-size":P,"--n-font-size-circle":r,"--n-font-weight-circle":K,"--n-icon-color":W,"--n-icon-size-circle":h,"--n-icon-size-line":x,"--n-line-bg-processing":U,"--n-rail-color":i,"--n-rail-height":g,"--n-text-color-circle":l,"--n-text-color-line-inner":V,"--n-text-color-line-outer":N}}),y=v?rt("progress",T(()=>e.status[0]),k,e):void 0;return{mergedClsPrefix:S,mergedIndicatorPlacement:s,gapDeg:m,cssVars:v?void 0:k,themeClass:y?.themeClass,onRender:y?.onRender}},render(){const{type:e,cssVars:s,indicatorTextColor:m,showIndicator:S,status:v,railColor:u,railStyle:k,color:y,percentage:z,viewBoxWidth:_,strokeWidth:P,mergedIndicatorPlacement:r,unit:i,borderRadius:g,fillBorderRadius:h,height:x,processing:l,circleGap:V,mergedClsPrefix:N,gapDeg:U,gapOffsetDegree:K,themeClass:W,$slots:q,onRender:re}=this;return re?.(),t(),p("div",{class:b([W,`${N}-progress`,`${N}-progress--${e}`,`${N}-progress--${v}`]),style:H(s),"aria-valuemax":100,"aria-valuemin":0,"aria-valuenow":z,role:e==="circle"||e==="line"||e==="dashboard"?"progressbar":"none"},[e==="circle"||e==="dashboard"?(t(),C(Cr,{key:0,clsPrefix:N,status:v,showIndicator:S,indicatorTextColor:m,railColor:u,fillColor:y,railStyle:k,offsetDegree:this.offsetDegree,percentage:z,viewBoxWidth:_,strokeWidth:P,gapDegree:U===void 0?e==="dashboard"?75:0:U,gapOffsetDegree:K,unit:i},Me(q),1032,["clsPrefix","status","showIndicator","indicatorTextColor","railColor","fillColor","railStyle","offsetDegree","percentage","viewBoxWidth","strokeWidth","gapDegree","gapOffsetDegree","unit"])):(t(),p(F,{key:1},[e==="line"?(t(),C($r,{key:0,clsPrefix:N,status:v,showIndicator:S,indicatorTextColor:m,railColor:u,fillColor:y,railStyle:k,percentage:z,processing:l,indicatorPlacement:r,unit:i,fillBorderRadius:h,railBorderRadius:g,height:x},Me(q),1032,["clsPrefix","status","showIndicator","indicatorTextColor","railColor","fillColor","railStyle","percentage","processing","indicatorPlacement","unit","fillBorderRadius","railBorderRadius","height"])):(t(),p(F,{key:1},[e==="multiple-circle"?(t(),C(Nr,{key:0,clsPrefix:N,strokeWidth:P,railColor:u,fillColor:y,railStyle:k,viewBoxWidth:_,percentage:z,showIndicator:S,circleGap:V},Me(q),1032,["clsPrefix","strokeWidth","railColor","fillColor","railStyle","viewBoxWidth","percentage","showIndicator","circleGap"])):$(()=>null)],64))],64))],14,Mr)}}),Ar=f("steps",`
 width: 100%;
 display: flex;
`,[f("step",`
 position: relative;
 display: flex;
 flex: 1;
 `,[Y("disabled","cursor: not-allowed"),Y("clickable",`
 cursor: pointer;
 `),O("&:last-child",[f("step-splitor","display: none;")])]),f("step-splitor",`
 background-color: var(--n-splitor-color);
 margin-top: calc(var(--n-step-header-font-size) / 2);
 height: 1px;
 flex: 1;
 align-self: flex-start;
 margin-left: 12px;
 margin-right: 12px;
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier);
 `),f("step-content","flex: 1;",[f("step-content-header",`
 color: var(--n-header-text-color);
 margin-top: calc(var(--n-indicator-size) / 2 - var(--n-step-header-font-size) / 2);
 line-height: var(--n-step-header-font-size);
 font-size: var(--n-step-header-font-size);
 position: relative;
 display: flex;
 font-weight: var(--n-step-header-font-weight);
 margin-left: 9px;
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier);
 `,[we("title",`
 white-space: nowrap;
 flex: 0;
 `)]),we("description",`
 color: var(--n-description-text-color);
 margin-top: 12px;
 margin-left: 9px;
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier);
 `)]),f("step-indicator",`
 background-color: var(--n-indicator-color);
 box-shadow: 0 0 0 1px var(--n-indicator-border-color);
 height: var(--n-indicator-size);
 width: var(--n-indicator-size);
 border-radius: 50%;
 display: flex;
 align-items: center;
 justify-content: center;
 transition:
 background-color .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier);
 `,[f("step-indicator-slot",`
 position: relative;
 width: var(--n-indicator-icon-size);
 height: var(--n-indicator-icon-size);
 font-size: var(--n-indicator-icon-size);
 line-height: var(--n-indicator-icon-size);
 `,[we("index",`
 display: inline-block;
 text-align: center;
 position: absolute;
 left: 0;
 top: 0;
 white-space: nowrap;
 font-size: var(--n-indicator-index-font-size);
 width: var(--n-indicator-icon-size);
 height: var(--n-indicator-icon-size);
 line-height: var(--n-indicator-icon-size);
 color: var(--n-indicator-text-color);
 transition: color .3s var(--n-bezier);
 `,[Ve()]),f("icon",`
 color: var(--n-indicator-text-color);
 transition: color .3s var(--n-bezier);
 `,[Ve()]),f("base-icon",`
 color: var(--n-indicator-text-color);
 transition: color .3s var(--n-bezier);
 `,[Ve()])])]),Y("vertical","flex-direction: column;",[je("show-description",[O(">",[f("step","padding-bottom: 8px;")])]),O(">",[f("step","margin-bottom: 16px;",[O("&:last-child","margin-bottom: 0;"),O(">",[f("step-indicator",[O(">",[f("step-splitor",`
 position: absolute;
 bottom: -8px;
 width: 1px;
 margin: 0 !important;
 left: calc(var(--n-indicator-size) / 2);
 height: calc(100% - var(--n-indicator-size));
 `)])]),f("step-content",[we("description","margin-top: 8px;")])])])])]),Y("content-bottom",[je("vertical",[O(">",[f("step","flex-direction: column",[O(">",[f("step-line","display: flex;",[O(">",[f("step-splitor",`
 margin-top: 0;
 align-self: center;
 `)])])]),O(">",[f("step-content","margin-top: calc(var(--n-indicator-size) / 2 - var(--n-step-header-font-size) / 2);",[f("step-content-header",`
 margin-left: 0;
 `),f("step-content__description",`
 margin-left: 0;
 `)])])])])])])]);function Or(e,s){return typeof e!="object"||e===null||Array.isArray(e)?null:(e.props||(e.props={}),e.props.internalIndex=s+1,e)}function Ur(e){return e.map((s,m)=>Or(s,m))}const Fr={...ge.props,current:Number,status:{type:String,default:"process"},size:{type:String,default:"medium"},vertical:Boolean,contentPlacement:{type:String,default:"right"},"onUpdate:current":[Function,Array],onUpdateCurrent:[Function,Array]},at=Dt("n-steps");var Wr=J({name:"Steps",props:Fr,slots:Object,setup(e,{slots:s}){const{mergedClsPrefixRef:m,mergedRtlRef:S}=Se(e),v=Ze("Steps",S,m),u=ge("Steps","-steps",Ar,Nt,e,m);return Mt(at,{props:e,mergedThemeRef:u,mergedClsPrefixRef:m,stepsSlots:s}),{mergedClsPrefix:m,rtlEnabled:v}},render(){const{mergedClsPrefix:e}=this;return t(),p("div",{class:b([`${e}-steps`,this.rtlEnabled&&`${e}-steps--rtl`,this.vertical&&`${e}-steps--vertical`,this.contentPlacement==="bottom"&&`${e}-steps--content-bottom`])},[$(()=>Ur(Bt(Zt(this))))],2)}});const qr=["onClick"],jr={status:String,title:String,description:String,disabled:Boolean,internalIndex:{type:Number,default:0}};var Oe=J({name:"Step",props:jr,slots:Object,setup(e){const s=Tt(at,null);s||At("step","`n-step` must be placed inside `n-steps`.");const{inlineThemeDisabled:m}=Se(),{props:S,mergedThemeRef:v,mergedClsPrefixRef:u,stepsSlots:k}=s,y=Ue(S,"vertical"),z=Ue(S,"contentPlacement"),_=T(()=>{const{status:i}=e;if(i)return i;{const{internalIndex:g}=e,{current:h}=S;if(h===void 0)return"process";if(g<h)return"finish";if(g===h)return S.status||"process";if(g>h)return"wait"}return"process"}),P=T(()=>{const{value:i}=_,{size:g}=S,{common:{cubicBezierEaseInOut:h},self:{stepHeaderFontWeight:x,[L("stepHeaderFontSize",g)]:l,[L("indicatorIndexFontSize",g)]:V,[L("indicatorSize",g)]:N,[L("indicatorIconSize",g)]:U,[L("indicatorTextColor",i)]:K,[L("indicatorBorderColor",i)]:W,[L("headerTextColor",i)]:q,[L("splitorColor",i)]:re,[L("indicatorColor",i)]:Z,[L("descriptionTextColor",i)]:ee}}=v.value;return{"--n-bezier":h,"--n-description-text-color":ee,"--n-header-text-color":q,"--n-indicator-border-color":W,"--n-indicator-color":Z,"--n-indicator-icon-size":U,"--n-indicator-index-font-size":V,"--n-indicator-size":N,"--n-indicator-text-color":K,"--n-splitor-color":re,"--n-step-header-font-size":l,"--n-step-header-font-weight":x}}),r=m?rt("step",T(()=>{const{value:i}=_,{size:g}=S;return`${i[0]}${g[0]}`}),P,S):void 0;return{stepsSlots:k,mergedClsPrefix:u,vertical:y,mergedStatus:_,handleStepClick:T(()=>{if(e.disabled)return;const{onUpdateCurrent:i,"onUpdate:current":g}=S;return i||g?()=>{i&&le(i,e.internalIndex),g&&le(g,e.internalIndex)}:void 0}),cssVars:m?void 0:P,themeClass:r?.themeClass,onRender:r?.onRender,contentPlacement:z}},render(){const{mergedClsPrefix:e,onRender:s,handleStepClick:m,disabled:S,contentPlacement:v,vertical:u}=this,k=be(this.$slots.default,r=>{const i=r||this.description;return i?(t(),p("div",{key:1,class:b(`${e}-step-content__description`)},[$(()=>i)],2)):null}),y=(t(),p("div",{class:b(`${e}-step-splitor`)},null,2)),z=(t(),p("div",{class:b(`${e}-step-indicator`),key:v},[I("div",{class:b(`${e}-step-indicator-slot`)},[c(Vt,null,{default:()=>be(this.$slots.icon,r=>{const{mergedStatus:i,stepsSlots:g}=this;return i==="finish"||i==="error"?i==="finish"?(t(),C(pe,{clsPrefix:e,key:"finish"},{default:()=>ye(g["finish-icon"],()=>[(t(),C(Gt))])},1032,["clsPrefix"])):i==="error"?(t(),C(pe,{clsPrefix:e,key:"error"},{default:()=>ye(g["error-icon"],()=>[(t(),C(Ot))])},1032,["clsPrefix"])):null:r||(t(),p("div",{key:this.internalIndex,class:b(`${e}-step-indicator-slot__index`)},[$(()=>this.internalIndex)],2))})},1024)],2),u?(t(),p(F,{key:0},[$(()=>y)],64)):$(()=>null)],2)),_=(t(),p("div",{class:b(`${e}-step-content`)},[I("div",{class:b(`${e}-step-content-header`)},[I("div",{class:b(`${e}-step-content-header__title`)},[$(()=>ye(this.$slots.title,()=>[this.title]))],2),!u&&v==="right"?(t(),p(F,{key:0},[$(()=>y)],64)):$(()=>null)],2),$(()=>k)],2));let P;return!u&&v==="bottom"?P=(r=>(t(),p(F,{key:5},[I("div",{class:b(`${e}-step-line`)},[$(()=>z),$(()=>y)],2),$(()=>_)],64)))():P=(r=>(t(),p(F,{key:6},[$(()=>z),$(()=>_)],64)))(),s?.(),t(),p("div",{class:b([`${e}-step`,S&&`${e}-step--disabled`,!S&&m&&`${e}-step--clickable`,this.themeClass,k&&`${e}-step--show-description`,`${e}-step--${this.mergedStatus}-status`]),style:H(this.cssVars),onClick:m},[$(()=>P)],14,qr)}});const Er="https://github.com/justindeelux/gotham/releases/latest/download",Hr=J({__name:"AddServerWizard",props:{show:{type:Boolean}},emits:["update:show","created"],setup(e,{emit:s}){const m=e,S=s,v=ot(),u=it(),k=`curl -fsSL ${Er}/install-agent.sh | sudo sh`,y=A(0),z=A(null),_=A(!1),P=A(!1),r=A(""),i=A(""),g=A(!1),h=A([]),x=A(null),l=qt({name:"",ip:"",port:22,sshUser:"root",keyMode:"new",keyName:"",privateKey:"",keyId:""}),V=T(()=>({name:{required:!0,message:"Name is required",trigger:["input","blur"]},ip:[{required:!0,message:"IP address is required",trigger:["input","blur"]},{validator:(M,n)=>K(n),message:"Enter a valid IPv4 address or hostname",trigger:["input","blur"]}],port:{type:"number",required:!0,message:"Port is required",trigger:["input","blur"]},sshUser:{required:!0,message:"SSH user is required",trigger:["input","blur"]},keyName:l.keyMode==="new"?{required:!0,message:"Key name is required",trigger:["input","blur"]}:[],privateKey:l.keyMode==="new"?{required:!0,message:"Private key is required",trigger:["input","blur"]}:[],keyId:l.keyMode==="existing"?{required:!0,message:"Key ID is required",trigger:["input","blur"]}:[]})),N=T(()=>{const M=x.value;return M?v.servers.find(n=>n.id===M.id)??M:null}),U=T(()=>N.value?.status==="ready");Ye(y,M=>{M===1&&x.value&&h.value.length===0&&q()});function K(M){const n=M.trim();if(n==="")return!1;const R=/^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/,$e=/^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/;return R.test(n)||$e.test(n)}async function W(){r.value="";try{await z.value?.validate()}catch{return}_.value=!0;try{let M=null;l.keyMode==="new"?M=(await ir({name:l.keyName.trim(),private_key:l.privateKey})).id:M=l.keyId.trim()||null;const n=await v.addServer({name:l.name.trim(),ip:l.ip.trim(),port:l.port??22,ssh_user:l.sshUser.trim(),ssh_key_id:M});x.value=n,S("created",n),y.value=1}catch(M){r.value=Ce(M)}finally{_.value=!1}}async function q(){const M=x.value;if(M){P.value=!0,i.value="";try{const n=await v.validate(M.id);h.value=n.checks,i.value=n.message,g.value=n.ok,n.ok&&u.success("Validation passed")}catch(n){i.value=Ce(n)}finally{P.value=!1}}}async function re(){try{await navigator.clipboard.writeText(k),u.success("Install command copied")}catch{u.error("Could not copy to clipboard")}}function Z(){S("update:show",!1),j()}function ee(M){S("update:show",M),M||j()}function j(){y.value=0,l.name="",l.ip="",l.port=22,l.sshUser="root",l.keyMode="new",l.keyName="",l.privateKey="",l.keyId="",r.value="",i.value="",g.value=!1,h.value=[],x.value=null,z.value?.restoreValidation()}return(M,n)=>(t(),C(a(Ut),{show:m.show,preset:"card",title:"Add server","mask-closable":!1,class:"wizard",style:{width:"680px","max-width":"94vw"},"onUpdate:show":ee},{footer:d(()=>[c(a(X),{justify:"end",size:8},{default:d(()=>[y.value===0?(t(),p(F,{key:0},[c(a(te),{onClick:Z},{default:d(()=>[...n[21]||(n[21]=[B("Cancel",-1)])]),_:1}),c(a(te),{type:"primary",loading:_.value,onClick:W},{default:d(()=>[...n[22]||(n[22]=[B(" Create & continue ",-1)])]),_:1},8,["loading"])],64)):y.value===1?(t(),p(F,{key:1},[c(a(te),{loading:P.value,onClick:q},{default:d(()=>[...n[23]||(n[23]=[B(" Retry validation ",-1)])]),_:1},8,["loading"]),c(a(te),{type:"primary",disabled:!g.value,onClick:n[8]||(n[8]=R=>y.value=2)},{default:d(()=>[...n[24]||(n[24]=[B(" Continue ",-1)])]),_:1},8,["disabled"])],64)):(t(),C(a(te),{key:2,type:"primary",onClick:Z},{default:d(()=>[...n[25]||(n[25]=[B("Done",-1)])]),_:1}))]),_:1})]),default:d(()=>[c(a(X),{vertical:"",size:20},{default:d(()=>[c(a(Wr),{current:y.value+1,size:"small"},{default:d(()=>[c(a(Oe),{title:"Connection",description:"Host and credentials"}),c(a(Oe),{title:"Validate",description:"Probe the node"}),c(a(Oe),{title:"Install",description:"Install the agent"})]),_:1},8,["current"]),r.value?(t(),C(a(ve),{key:0,type:"error","show-icon":!0},{default:d(()=>[B(oe(r.value),1)]),_:1})):ae("",!0),y.value===0?(t(),C(a(nr),{key:1,ref_key:"formRef",ref:z,model:l,rules:V.value,"label-placement":"top",onSubmit:Ft(W,["prevent"])},{default:d(()=>[c(a(X),{vertical:"",size:4},{default:d(()=>[c(a(ne),{label:"Name",path:"name"},{default:d(()=>[c(a(se),{value:l.name,"onUpdate:value":n[0]||(n[0]=R=>l.name=R),placeholder:"web-1"},null,8,["value"])]),_:1}),c(a(X),{size:12},{default:d(()=>[c(a(ne),{label:"IP address",path:"ip",class:"grow"},{default:d(()=>[c(a(se),{value:l.ip,"onUpdate:value":n[1]||(n[1]=R=>l.ip=R),placeholder:"10.0.0.5"},null,8,["value"])]),_:1}),c(a(ne),{label:"Port",path:"port",style:{width:"120px"}},{default:d(()=>[c(a(hr),{value:l.port,"onUpdate:value":n[2]||(n[2]=R=>l.port=R),min:1,max:65535,placeholder:"22"},null,8,["value"])]),_:1})]),_:1}),c(a(ne),{label:"SSH user",path:"sshUser"},{default:d(()=>[c(a(se),{value:l.sshUser,"onUpdate:value":n[3]||(n[3]=R=>l.sshUser=R),placeholder:"root"},null,8,["value"])]),_:1}),c(a(ne),{label:"SSH key"},{default:d(()=>[c(a(Xt),{value:l.keyMode,"onUpdate:value":n[4]||(n[4]=R=>l.keyMode=R),size:"small"},{default:d(()=>[c(a(He),{value:"new"},{default:d(()=>[...n[9]||(n[9]=[B("Paste a new key",-1)])]),_:1}),c(a(He),{value:"existing"},{default:d(()=>[...n[10]||(n[10]=[B("Use an existing key ID",-1)])]),_:1})]),_:1},8,["value"])]),_:1}),l.keyMode==="new"?(t(),p(F,{key:0},[c(a(ne),{label:"Key name",path:"keyName"},{default:d(()=>[c(a(se),{value:l.keyName,"onUpdate:value":n[5]||(n[5]=R=>l.keyName=R),placeholder:"deploy-key"},null,8,["value"])]),_:1}),c(a(ne),{label:"Private key (PEM)",path:"privateKey"},{default:d(()=>[c(a(se),{value:l.privateKey,"onUpdate:value":n[6]||(n[6]=R=>l.privateKey=R),type:"textarea",autosize:{minRows:4,maxRows:10},placeholder:"-----BEGIN OPENSSH PRIVATE KEY-----"},null,8,["value"])]),_:1}),c(a(G),{depth:"3"},{default:d(()=>[...n[11]||(n[11]=[B(" The key is encrypted at rest by the control plane. It is never returned by the API. ",-1)])]),_:1})],64)):(t(),C(a(ne),{key:1,label:"Key ID",path:"keyId"},{default:d(()=>[c(a(se),{value:l.keyId,"onUpdate:value":n[7]||(n[7]=R=>l.keyId=R),placeholder:"00000000-0000-0000-0000-000000000000"},null,8,["value"])]),_:1})),c(a(G),{depth:"3"},{default:d(()=>[...n[12]||(n[12]=[B(" Key listing is not exposed by the API yet, so paste the key material or an existing key ID. Password-auth servers are not supported in this release. ",-1)])]),_:1})]),_:1})]),_:1},8,["model","rules"])):y.value===1?(t(),C(a(X),{key:2,vertical:"",size:12},{default:d(()=>[c(a(G),{depth:"2"},{default:d(()=>[...n[13]||(n[13]=[B(" Running readiness probes over SSH. Docker must be installed and reachable. ",-1)])]),_:1}),i.value&&!g.value?(t(),C(a(ve),{key:0,type:"error","show-icon":!0},{default:d(()=>[B(oe(i.value),1)]),_:1})):ae("",!0),h.value.length?(t(),C(a(X),{key:1,vertical:"",size:8},{default:d(()=>[(t(!0),p(F,null,Wt(h.value,R=>(t(),p("div",{key:R.name,class:"check-row"},[c(a(rr),{type:R.ok?"success":"error",size:"small",round:""},{default:d(()=>[B(oe(R.ok?"ok":"fail"),1)]),_:2},1032,["type"]),c(a(G),{strong:"",class:"check-name"},{default:d(()=>[B(oe(R.name.toUpperCase()),1)]),_:2},1024),c(a(G),{depth:"2",class:"check-detail"},{default:d(()=>[B(oe(R.detail),1)]),_:2},1024)]))),128))]),_:1})):P.value?ae("",!0):(t(),C(a(G),{key:2,depth:"3"},{default:d(()=>[...n[14]||(n[14]=[B("No checks have run yet.",-1)])]),_:1})),g.value?(t(),C(a(ve),{key:3,type:"success","show-icon":!0},{default:d(()=>[...n[15]||(n[15]=[B(" All checks passed. Continue to install the node agent. ",-1)])]),_:1})):ae("",!0)]),_:1})):(t(),C(a(X),{key:3,vertical:"",size:12},{default:d(()=>[c(a(X),{align:"center",size:8},{default:d(()=>[c(a(G),{depth:"2"},{default:d(()=>[...n[16]||(n[16]=[B("Current status:",-1)])]),_:1}),N.value?(t(),C(nt,{key:0,status:N.value.status},null,8,["status"])):ae("",!0)]),_:1}),U.value?(t(),C(a(ve),{key:0,type:"success","show-icon":!0},{default:d(()=>[...n[17]||(n[17]=[B(" The agent registered and the server is ready. ",-1)])]),_:1})):ae("",!0),c(a(G),{depth:"2"},{default:d(()=>[...n[18]||(n[18]=[B(" SSH into the node and run the installer. It downloads the agent, installs the systemd unit, and registers with the control plane. ",-1)])]),_:1}),c(a(X),{align:"center",size:8},{default:d(()=>[c(a(se),{value:k,readonly:"",class:"grow"}),c(a(te),{onClick:re},{default:d(()=>[...n[19]||(n[19]=[B("Copy",-1)])]),_:1})]),_:1}),c(a(G),{depth:"3"},{default:d(()=>[...n[20]||(n[20]=[B(" The page keeps polling every 5 seconds; the status badge flips to Ready once the agent checks in. ",-1)])]),_:1}),N.value?(t(),C(a(G),{key:1,depth:"3"},{default:d(()=>[B(" Detected memory: "+oe(a(Ee)(N.value.total_mem))+" · disk: "+oe(a(Ee)(N.value.total_disk)),1)]),_:1})):ae("",!0)]),_:1}))]),_:1})]),_:1},8,["show"]))}}),Kr=ar(Hr,[["__scopeId","data-v-0ff1a4c0"]]),ci=J({__name:"ServersPage",setup(e){const s=ot(),m=it(),S=A(!1),v=A(null);function u(r){return r==null?ce(G,{depth:3},{default:()=>"—"}):ce(Tr,{type:"line",percentage:Math.round(Math.min(Math.max(r,0),100)),height:14})}function k(r){return ce(X,{size:8,align:"center",wrap:!1},{default:()=>[ce(te,{size:"small",loading:v.value===r.id,onClick:()=>{_(r)}},{default:()=>"Validate"}),ce(er,{onPositiveClick:()=>{P(r)}},{trigger:()=>ce(te,{size:"small",type:"error",quaternary:!0},{default:()=>"Delete"}),default:()=>`Delete server "${r.name}"?`})]})}const y=[{title:"Name",key:"name",minWidth:140,ellipsis:{tooltip:!0}},{title:"Address",key:"address",minWidth:150,render:r=>`${r.ip}:${r.port}`},{title:"Status",key:"status",width:120,render:r=>ce(nt,{status:r.status})},{title:"CPU",key:"cpu_usage",width:140,render:r=>u(r.cpu_usage)},{title:"RAM",key:"mem_usage",width:140,render:r=>u(r.mem_usage)},{title:"Disk",key:"disk_usage",width:140,render:r=>u(r.disk_usage)},{title:"Docker",key:"docker_version",minWidth:120,render:r=>r.docker_version??"—"},{title:"Last seen",key:"last_seen",width:120,render:r=>Qt(r.last_seen)},{title:"Actions",key:"actions",width:190,render:r=>k(r)}];function z(r){return r.id}async function _(r){v.value=r.id;try{const i=await s.validate(r.id);if(i.ok){m.success(`${r.name}: validation passed`);return}const g=i.checks.filter(h=>!h.ok).map(h=>h.name).join(", ");m.error(i.message||`${r.name}: failed checks: ${g}`)}catch(i){m.error(Ce(i))}finally{v.value=null}}async function P(r){try{await s.removeServer(r.id),m.success(`Deleted ${r.name}`)}catch(i){m.error(Ce(i))}}return jt(()=>{s.fetchServers().catch(()=>{}),s.pollServers()}),Et(()=>{s.stopPolling()}),(r,i)=>(t(),C(a(X),{vertical:"",size:16},{default:d(()=>[c(a(Ht),null,{header:d(()=>[c(a(X),{align:"center",justify:"space-between"},{default:d(()=>[c(a(G),{strong:""},{default:d(()=>[...i[2]||(i[2]=[B("Servers",-1)])]),_:1}),c(a(te),{type:"primary",onClick:i[0]||(i[0]=g=>S.value=!0)},{default:d(()=>[...i[3]||(i[3]=[B(" Add server ",-1)])]),_:1})]),_:1})]),default:d(()=>[a(s).error?(t(),C(a(ve),{key:0,type:"error","show-icon":!0,style:{"margin-bottom":"12px"}},{default:d(()=>[B(oe(a(s).error),1)]),_:1})):ae("",!0),c(a(Yt),{columns:y,data:a(s).servers,loading:a(s).loading,"row-key":z,bordered:!1,pagination:{pageSize:10}},null,8,["data","loading"])]),_:1}),c(Kr,{show:S.value,"onUpdate:show":i[1]||(i[1]=g=>S.value=g)},null,8,["show"])]),_:1}))}});export{ci as default};
