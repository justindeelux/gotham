import{A as ke}from"./Alert-4dm8qYL3.js";import{d as U,o as t,c as m,a as I,H as b,G as P,Z as ve,a8 as rt,a9 as Nt,aa as Tt,ab as Mt,ac as Dt,x as A,y,E as de,j as x,J as ye,ad as Vt,q as O,ae as ce,a0 as it,af as nt,M as D,ag as te,ah as At,ai as He,aj as Ot,Y as ge,ak as he,al as Ke,am as we,U as ot,an as Ut,ao as Le,ap as Fe,B as Q,aq as qe,I as j,_ as at,K as We,X as pe,ar as Ft,as as jt,at as qt,au as Pe,av as st,F as q,aw as lt,ax as dt,ay as ut,az as Wt,A as Z,aA as Et,Q as G,aB as Ve,aC as Ge,aD as Ht,aE as Kt,e as v,aF as Lt,aG as Gt,aH as Xt,u as s,w as h,k as R,t as ie,aI as Yt,l as se,m as Zt,r as Jt,s as Qt,g as er,a5 as tr,C as rr,a2 as fe}from"./index-4iWiJHli.js";import{s as ir,r as nr,C as or,R as ar,D as sr}from"./DataTable-JKAELm5F.js";import{u as ct}from"./use-message-CzBW3JDP.js";import{g as lr,S as Y,t as X}from"./text-CtSLgTaB.js";import{p as dr,P as ur,a as ft,c as cr,d as $e}from"./servers-CScFfLQ5.js";import{u as je}from"./use-locale-D7tjoEjN.js";import{I as le,u as fr,f as me}from"./Input-Lfsw8Bpt.js";import{F as pr,a as ae}from"./FormItem-D-76LpBK.js";import{u as pt}from"./servers-fYFyjn0Y.js";import{_ as gr}from"./_plugin-vue_export-helper-DlAUqK2U.js";import"./Empty-BB06ROGj.js";const hr=["value","name","checked","disabled","onChange","onFocus","onBlur"];var Xe=U({name:"RadioButton",props:nr,setup:ir,render(){const{mergedClsPrefix:e}=this;return t(),m("label",{class:b([`${e}-radio-button`,this.mergedDisabled&&`${e}-radio-button--disabled`,this.renderSafeChecked&&`${e}-radio-button--checked`,this.focus&&[`${e}-radio-button--focus`]])},[I("input",{ref:"inputRef",type:"radio",class:b(`${e}-radio-input`),value:this.value,name:this.mergedName,checked:this.renderSafeChecked,disabled:this.mergedDisabled,onChange:this.handleRadioInputChange,onFocus:this.handleRadioInputFocus,onBlur:this.handleRadioInputBlur},null,42,hr),I("div",{class:b(`${e}-radio-button__state-border`)},null,2),P(()=>ve(this.$slots.default,r=>!r&&!this.label?null:(t(),m("div",{ref:"labelRef",class:b(`${e}-radio__label`)},[P(()=>r||this.label)],2))))],2)}}),mr=U({name:"Add",render(){return(()=>{const e=rt("b30130fbba5c5b23");return e[0]||(e[0]=I("svg",{width:"512",height:"512",viewBox:"0 0 512 512",fill:"none",xmlns:"http://www.w3.org/2000/svg"},[I("path",{d:"M256 112V400M400 256H112",stroke:"currentColor","stroke-width":"32","stroke-linecap":"round","stroke-linejoin":"round"})],-1))})()}}),vr=U({name:"Remove",render(){return(()=>{const e=rt("a77472467b8adb0a");return e[0]||(e[0]=I("svg",{xmlns:"http://www.w3.org/2000/svg",viewBox:"0 0 512 512"},[I("line",{x1:"400",y1:"256",x2:"112",y2:"256",style:`
        fill: none;
        stroke: currentColor;
        stroke-linecap: round;
        stroke-linejoin: round;
        stroke-width: 32px;
      `})],-1))})()}});function yr(e){const{textColorDisabled:r}=e;return{iconColorDisabled:r}}const br=Nt({name:"InputNumber",common:Dt,peers:{Button:Mt,Input:Tt},self:yr});var xr=A([y("input-number-suffix",`
 display: inline-block;
 margin-right: 10px;
 `),y("input-number-prefix",`
 display: inline-block;
 margin-left: 10px;
 `)]);function kr(e){return e==null||typeof e=="string"&&e.trim()===""?null:Number(e)}function wr(e){return e.includes(".")&&(/^(-)?\d+.*(\.|0)$/.test(e)||/^-?\d*$/.test(e))||e==="-"||e==="-0"}function Ae(e){return e==null?!0:!Number.isNaN(e)}function Ye(e,r){return typeof e!="number"?"":r===void 0?String(e):e.toFixed(r)}function Oe(e){if(e===null)return null;if(typeof e=="number")return e;{const r=Number(e);return Number.isNaN(r)?null:r}}const Ze=800,Je=100,Cr={...de.props,autofocus:Boolean,loading:{type:Boolean,default:void 0},placeholder:String,defaultValue:{type:Number,default:null},value:Number,step:{type:[Number,String],default:1},min:[Number,String],max:[Number,String],size:String,disabled:{type:Boolean,default:void 0},validator:Function,bordered:{type:Boolean,default:void 0},showButton:{type:Boolean,default:!0},buttonPlacement:{type:String,default:"right"},inputProps:Object,readonly:Boolean,clearable:Boolean,keyboard:{type:Object,default:{}},updateValueOnInput:{type:Boolean,default:!0},round:{type:Boolean,default:void 0},parse:Function,format:Function,precision:Number,status:String,"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],onFocus:[Function,Array],onBlur:[Function,Array],onClear:[Function,Array],onChange:[Function,Array]};var Sr=U({name:"InputNumber",props:Cr,slots:Object,setup(e){const{mergedBorderedRef:r,mergedClsPrefixRef:l,mergedRtlRef:d,mergedComponentPropsRef:c}=ye(e),u=de("InputNumber","-input-number",xr,br,e,l),{localeRef:p}=je("InputNumber"),g=Vt(e,{mergedSize:a=>{const{size:$}=e;if($)return $;const{mergedSize:T}=a||{};if(T?.value)return T.value;const K=c?.value?.InputNumber?.size;return K||"medium"}}),{mergedSizeRef:C,mergedDisabledRef:z,mergedStatusRef:_}=g,i=O(null),n=O(null),k=O(null),w=O(e.defaultValue),S=we(e,"value"),f=fr(S,w),V=O(""),N=a=>{const $=String(a).split(".")[1];return $?$.length:0},F=a=>{const $=[e.min,e.max,e.step,a].map(T=>T===void 0?0:N(T));return Math.max(...$)},L=ce(()=>{const{placeholder:a}=e;return a!==void 0?a:p.value.placeholder}),W=ce(()=>{const a=Oe(e.step);return a!==null?a===0?1:Math.abs(a):1}),E=ce(()=>{const a=Oe(e.min);return a!==null?a:null}),ne=ce(()=>{const a=Oe(e.max);return a!==null?a:null}),J=()=>{const{value:a}=f;if(Ae(a)){const{format:$,precision:T}=e;$?V.value=$(a):a===null||T===void 0||N(a)>T?V.value=Ye(a,void 0):V.value=Ye(a,T)}else V.value=String(a)};J();const re=a=>{const{value:$}=f;if(a===$){J();return}const{"onUpdate:value":T,onUpdateValue:K,onChange:ee}=e,{nTriggerFormInput:ue,nTriggerFormChange:Ne}=g;ee&&te(ee,a),K&&te(K,a),T&&te(T,a),w.value=a,ue(),Ne()},H=({offset:a,doUpdateIfValid:$,fixPrecision:T,isInputing:K})=>{const{value:ee}=V;if(K&&wr(ee))return!1;const ue=(e.parse||kr)(ee);if(ue===null)return $&&re(null),null;if(Ae(ue)){const Ne=N(ue),{precision:Te}=e;if(Te!==void 0&&Te<Ne&&!T)return!1;let oe=Number.parseFloat((ue+a).toFixed(Te??F(ue)));if(Ae(oe)){const{value:Me}=ne,{value:De}=E;if(Me!==null&&oe>Me){if(!$||K)return!1;oe=Me}if(De!==null&&oe<De){if(!$||K)return!1;oe=De}return e.validator&&!e.validator(oe)?!1:($&&re(oe),oe)}}return!1},M=ce(()=>H({offset:0,doUpdateIfValid:!1,isInputing:!1,fixPrecision:!1})===!1),o=ce(()=>{const{value:a}=f;if(e.validator&&a===null)return!1;const{value:$}=W;return H({offset:-$,doUpdateIfValid:!1,isInputing:!1,fixPrecision:!1})!==!1}),B=ce(()=>{const{value:a}=f;if(e.validator&&a===null)return!1;const{value:$}=W;return H({offset:+$,doUpdateIfValid:!1,isInputing:!1,fixPrecision:!1})!==!1});function ze(a){const{onFocus:$}=e,{nTriggerFormFocus:T}=g;$&&te($,a),T()}function yt(a){if(a.target===i.value?.wrapperElRef)return;const $=H({offset:0,doUpdateIfValid:!0,isInputing:!1,fixPrecision:!0});if($!==!1){const ee=i.value?.inputElRef;ee&&(ee.value=String($||"")),f.value===$&&J()}else J();const{onBlur:T}=e,{nTriggerFormBlur:K}=g;T&&te(T,a),K(),At(()=>{J()})}function bt(a){const{onClear:$}=e;$&&te($,a)}function _e(){const{value:a}=B;if(!a){Re();return}const{value:$}=f;if($===null)e.validator||re(Ee());else{const{value:T}=W;H({offset:T,doUpdateIfValid:!0,isInputing:!1,fixPrecision:!0})}}function Ie(){const{value:a}=o;if(!a){Be();return}const{value:$}=f;if($===null)e.validator||re(Ee());else{const{value:T}=W;H({offset:-T,doUpdateIfValid:!0,isInputing:!1,fixPrecision:!0})}}const xt=ze,kt=yt;function Ee(){if(e.validator)return null;const{value:a}=E,{value:$}=ne;return a!==null?Math.max(0,a):$!==null?Math.min(0,$):0}function wt(a){bt(a),re(null)}function Ct(a){k.value?.$el.contains(a.target)&&a.preventDefault(),n.value?.$el.contains(a.target)&&a.preventDefault(),i.value?.activate()}let be=null,xe=null,Ce=null;function Be(){Ce&&(window.clearTimeout(Ce),Ce=null),be&&(window.clearInterval(be),be=null)}let Se=null;function Re(){Se&&(window.clearTimeout(Se),Se=null),xe&&(window.clearInterval(xe),xe=null)}function St(){Be(),Ce=window.setTimeout(()=>{be=window.setInterval(()=>{Ie()},Je)},Ze),He("mouseup",document,Be,{once:!0})}function Pt(){Re(),Se=window.setTimeout(()=>{xe=window.setInterval(()=>{_e()},Je)},Ze),He("mouseup",document,Re,{once:!0})}const $t=()=>{xe||_e()},zt=()=>{be||Ie()};function _t(a){if(a.key==="Enter"){if(a.target===i.value?.wrapperElRef)return;H({offset:0,doUpdateIfValid:!0,isInputing:!1,fixPrecision:!0})!==!1&&i.value?.deactivate()}else if(a.key==="ArrowUp"){if(!B.value||e.keyboard.ArrowUp===!1)return;a.preventDefault(),H({offset:0,doUpdateIfValid:!0,isInputing:!1,fixPrecision:!0})!==!1&&_e()}else if(a.key==="ArrowDown"){if(!o.value||e.keyboard.ArrowDown===!1)return;a.preventDefault(),H({offset:0,doUpdateIfValid:!0,isInputing:!1,fixPrecision:!0})!==!1&&Ie()}}function It(a){V.value=a,e.updateValueOnInput&&!e.format&&!e.parse&&e.precision===void 0&&H({offset:0,doUpdateIfValid:!0,isInputing:!0,fixPrecision:!1})}it(f,()=>{J()});const Bt={focus:()=>i.value?.focus(),blur:()=>i.value?.blur(),select:()=>i.value?.select()},Rt=nt("InputNumber",d,l);return{...Bt,rtlEnabled:Rt,inputInstRef:i,minusButtonInstRef:n,addButtonInstRef:k,mergedClsPrefix:l,mergedBordered:r,uncontrolledValue:w,mergedValue:f,mergedPlaceholder:L,displayedValueInvalid:M,mergedSize:C,mergedDisabled:z,displayedValue:V,addable:B,minusable:o,mergedStatus:_,handleFocus:xt,handleBlur:kt,handleClear:wt,handleMouseDown:Ct,handleAddClick:$t,handleMinusClick:zt,handleAddMousedown:Pt,handleMinusMousedown:St,handleKeyDown:_t,handleUpdateDisplayedValue:It,mergedTheme:u,inputThemeOverrides:{paddingSmall:"0 8px 0 10px",paddingMedium:"0 8px 0 12px",paddingLarge:"0 8px 0 14px"},buttonThemeOverrides:D(()=>{const{self:{iconColorDisabled:a}}=u.value,[$,T,K,ee]=Ot(a);return{textColorTextDisabled:`rgb(${$}, ${T}, ${K})`,opacityDisabled:`${ee}`}})}},render(){const{mergedClsPrefix:e,$slots:r}=this,l=()=>(t(),x(Ke,{text:!0,disabled:!this.minusable||this.mergedDisabled||this.readonly,focusable:!1,theme:this.mergedTheme.peers.Button,themeOverrides:this.mergedTheme.peerOverrides.Button,builtinThemeOverrides:this.buttonThemeOverrides,onClick:this.handleMinusClick,onMousedown:this.handleMinusMousedown,ref:"minusButtonInstRef"},{icon:()=>ge(r["minus-icon"],()=>[(t(),x(he,{clsPrefix:e},{default:()=>(t(),x(vr))},1032,["clsPrefix"]))])},1032,["disabled","theme","themeOverrides","builtinThemeOverrides","onClick","onMousedown"])),d=()=>(t(),x(Ke,{text:!0,disabled:!this.addable||this.mergedDisabled||this.readonly,focusable:!1,theme:this.mergedTheme.peers.Button,themeOverrides:this.mergedTheme.peerOverrides.Button,builtinThemeOverrides:this.buttonThemeOverrides,onClick:this.handleAddClick,onMousedown:this.handleAddMousedown,ref:"addButtonInstRef"},{icon:()=>ge(r["add-icon"],()=>[(t(),x(he,{clsPrefix:e},{default:()=>(t(),x(mr))},1032,["clsPrefix"]))])},1032,["disabled","theme","themeOverrides","builtinThemeOverrides","onClick","onMousedown"]));return t(),m("div",{class:b([`${e}-input-number`,this.rtlEnabled&&`${e}-input-number--rtl`])},[(t(),x(le,{ref:"inputInstRef",autofocus:this.autofocus,status:this.mergedStatus,bordered:this.mergedBordered,loading:this.loading,value:this.displayedValue,onUpdateValue:this.handleUpdateDisplayedValue,theme:this.mergedTheme.peers.Input,themeOverrides:this.mergedTheme.peerOverrides.Input,builtinThemeOverrides:this.inputThemeOverrides,size:this.mergedSize,placeholder:this.mergedPlaceholder,disabled:this.mergedDisabled,readonly:this.readonly,round:this.round,textDecoration:this.displayedValueInvalid?"line-through":void 0,onFocus:this.handleFocus,onBlur:this.handleBlur,onKeydown:this.handleKeyDown,onMousedown:this.handleMouseDown,onClear:this.handleClear,clearable:this.clearable,inputProps:this.inputProps,internalLoadingBeforeSuffix:!0},{prefix:()=>this.showButton&&this.buttonPlacement==="both"?[l(),ve(r.prefix,c=>c?(t(),m("span",{key:1,class:b(`${e}-input-number-prefix`)},[P(()=>c)],2)):null)]:r.prefix?.(),suffix:()=>this.showButton?[ve(r.suffix,c=>c?(t(),m("span",{key:2,class:b(`${e}-input-number-suffix`)},[P(()=>c)],2)):null),this.buttonPlacement==="right"?l():null,d()]:r.suffix?.()},1032,["autofocus","status","bordered","loading","value","onUpdateValue","theme","themeOverrides","builtinThemeOverrides","size","placeholder","disabled","readonly","round","textDecoration","onFocus","onBlur","onKeydown","onMousedown","onClear","clearable","inputProps"]))],2)}});const gt=ot("n-popconfirm"),ht={positiveText:String,negativeText:String,showIcon:{type:Boolean,default:!0},onPositiveClick:{type:Function,required:!0},onNegativeClick:{type:Function,required:!0}},Qe=Ut(ht);var Pr=U({name:"NPopconfirmPanel",props:ht,setup(e){const{localeRef:r}=je("Popconfirm"),{inlineThemeDisabled:l}=ye(),{mergedClsPrefixRef:d,mergedThemeRef:c,props:u}=at(gt),p=D(()=>{const{common:{cubicBezierEaseInOut:C},self:{fontSize:z,iconSize:_,iconColor:i}}=c.value;return{"--n-bezier":C,"--n-font-size":z,"--n-icon-size":_,"--n-icon-color":i}}),g=l?We("popconfirm-panel",void 0,p,u):void 0;return{...je("Popconfirm"),mergedClsPrefix:d,cssVars:l?void 0:p,localizedPositiveText:D(()=>e.positiveText||r.value.positiveText),localizedNegativeText:D(()=>e.negativeText||r.value.negativeText),positiveButtonProps:we(u,"positiveButtonProps"),negativeButtonProps:we(u,"negativeButtonProps"),handlePositiveClick(C){e.onPositiveClick(C)},handleNegativeClick(C){e.onNegativeClick(C)},themeClass:g?.themeClass,onRender:g?.onRender}},render(){const{mergedClsPrefix:e,showIcon:r,$slots:l}=this,d=ge(l.action,()=>this.negativeText===null&&this.positiveText===null?[]:[this.negativeText!==null&&(t(),x(Q,Fe({key:1,size:"small",onClick:this.handleNegativeClick},this.negativeButtonProps),{_:1,default:Le(()=>this.localizedNegativeText)},16,["onClick"])),this.positiveText!==null&&(t(),x(Q,Fe({key:2,size:"small",type:"primary",onClick:this.handlePositiveClick},this.positiveButtonProps),{_:1,default:Le(()=>this.localizedPositiveText)},16,["onClick"]))]);return this.onRender?.(),t(),m("div",{class:b([`${e}-popconfirm__panel`,this.themeClass]),style:j(this.cssVars)},[P(()=>ve(l.default,c=>r||c?(t(),m("div",{key:3,class:b(`${e}-popconfirm__body`)},[r?(t(),m("div",{key:0,class:b(`${e}-popconfirm__icon`)},[P(()=>ge(l.icon,()=>[(t(),x(he,{clsPrefix:e},{default:()=>(t(),x(qe))},1032,["clsPrefix"]))]))],2)):P(()=>null),P(()=>c)],2)):null)),d?(t(),m("div",{key:0,class:b([`${e}-popconfirm__action`])},[P(()=>d)],2)):P(()=>null)],6)}}),$r=y("popconfirm",[pe("body",`
 font-size: var(--n-font-size);
 display: flex;
 align-items: center;
 flex-wrap: nowrap;
 position: relative;
 `,[pe("icon",`
 display: flex;
 font-size: var(--n-icon-size);
 color: var(--n-icon-color);
 transition: color .3s var(--n-bezier);
 margin: 0 8px 0 0;
 `)]),pe("action",`
 display: flex;
 justify-content: flex-end;
 `,[A("&:not(:first-child)","margin-top: 8px"),y("button",[A("&:not(:last-child)","margin-right: 8px;")])])]);const zr={...de.props,...dr,positiveText:String,negativeText:String,showIcon:{type:Boolean,default:!0},trigger:{type:String,default:"click"},positiveButtonProps:Object,negativeButtonProps:Object,onPositiveClick:Function,onNegativeClick:Function};var _r=U({name:"Popconfirm",props:zr,slots:Object,__popover__:!0,setup(e){const{mergedClsPrefixRef:r}=ye(),l=de("Popconfirm","-popconfirm",$r,jt,e,r),d=O(null);function c(p){if(!d.value?.getMergedShow())return;const{onPositiveClick:g,"onUpdate:show":C}=e;Promise.resolve(g?g(p):!0).then(z=>{z!==!1&&(d.value?.setShow(!1),C&&te(C,!1))})}function u(p){if(!d.value?.getMergedShow())return;const{onNegativeClick:g,"onUpdate:show":C}=e;Promise.resolve(g?g(p):!0).then(z=>{z!==!1&&(d.value?.setShow(!1),C&&te(C,!1))})}return st(gt,{mergedThemeRef:l,mergedClsPrefixRef:r,props:e}),{setShow(p){d.value?.setShow(p)},syncPosition(){d.value?.syncPosition()},mergedTheme:l,popoverInstRef:d,handlePositiveClick:c,handleNegativeClick:u}},render(){const{$slots:e,$props:r,mergedTheme:l}=this;return t(),x(ur,Fe(Ft(r,Qe),{theme:l.peers.Popover,themeOverrides:l.peerOverrides.Popover,internalExtraClass:["popconfirm"],ref:"popoverInstRef"}),{trigger:e.trigger,default:()=>{const d=qt(r,Qe);return t(),x(Pr,{...d,onPositiveClick:this.handlePositiveClick,onNegativeClick:this.handleNegativeClick},Pe(e),1040)}},1040,["theme","themeOverrides"])}});const Ir=["id"],Br=["stop-color"],Rr=["stop-color"],Nr=["viewBox"],Tr=["d","stroke-width"],Mr=["d","stroke-width"],Dr={success:(t(),x(ut)),error:(t(),x(dt)),warning:(t(),x(qe)),info:(t(),x(lt))};var Vr=U({name:"ProgressCircle",props:{clsPrefix:{type:String,required:!0},status:{type:String,required:!0},strokeWidth:{type:Number,required:!0},fillColor:[String,Object],railColor:String,railStyle:[String,Object],percentage:{type:Number,default:0},offsetDegree:{type:Number,default:0},showIndicator:{type:Boolean,required:!0},indicatorTextColor:String,unit:String,viewBoxWidth:{type:Number,required:!0},gapDegree:{type:Number,required:!0},gapOffsetDegree:{type:Number,default:0}},setup(e,{slots:r}){const l=D(()=>{const u="gradient",{fillColor:p}=e;return typeof p=="object"?`${u}-${Wt(JSON.stringify(p))}`:u});function d(u,p,g,C){const{gapDegree:z,viewBoxWidth:_,strokeWidth:i}=e,n=50,k=0,w=n,S=0,f=100,V=50+i/2,N=`M ${V},${V} m ${k},${w}
      a ${n},${n} 0 1 1 ${S},-100
      a ${n},${n} 0 1 1 0,${f}`,F=Math.PI*2*n;return{pathString:N,pathStyle:{stroke:C==="rail"?g:typeof e.fillColor=="object"?`url(#${l.value})`:g,strokeDasharray:`${Math.min(u,100)/100*(F-z)}px ${_*8}px`,strokeDashoffset:`-${z/2}px`,transformOrigin:p?"center":void 0,transform:p?`rotate(${p}deg)`:void 0}}}const c=()=>{const u=typeof e.fillColor=="object",p=u?e.fillColor.stops[0]:"",g=u?e.fillColor.stops[1]:"";return u&&(t(),m("defs",null,[I("linearGradient",{id:l.value,x1:"0%",y1:"100%",x2:"100%",y2:"0%"},[I("stop",{offset:"0%","stop-color":p},null,8,Br),I("stop",{offset:"100%","stop-color":g},null,8,Rr)],8,Ir)]))};return()=>{const{fillColor:u,railColor:p,strokeWidth:g,offsetDegree:C,status:z,percentage:_,showIndicator:i,indicatorTextColor:n,unit:k,gapOffsetDegree:w,clsPrefix:S}=e,{pathString:f,pathStyle:V}=d(100,0,p,"rail"),{pathString:N,pathStyle:F}=d(_,C,u,"fill"),L=100+g;return t(),m("div",{class:b(`${S}-progress-content`),role:"none"},[I("div",{class:b(`${S}-progress-graph`),"aria-hidden":!0},[I("div",{class:b(`${S}-progress-graph-circle`),style:j({transform:w?`rotate(${w}deg)`:void 0})},[(t(),m("svg",{viewBox:`0 0 ${L} ${L}`},[P(()=>c()),I("g",null,[I("path",{class:b(`${S}-progress-graph-circle-rail`),d:f,"stroke-width":g,"stroke-linecap":"round",fill:"none",style:j(V)},null,14,Tr)]),I("g",null,[I("path",{class:b([`${S}-progress-graph-circle-fill`,_===0&&`${S}-progress-graph-circle-fill--empty`]),d:N,"stroke-width":g,"stroke-linecap":"round",fill:"none",style:j(F)},null,14,Mr)])],8,Nr))],6)],2),i?(t(),m("div",{key:0},[r.default?(t(),m("div",{key:0,class:b(`${S}-progress-custom-content`),role:"none"},[P(()=>r.default())],2)):(t(),m(q,{key:1},[z!=="default"?(t(),m("div",{key:0,class:b(`${S}-progress-icon`),"aria-hidden":!0},[(t(),x(he,{clsPrefix:S},{default:()=>Dr[z]},1032,["clsPrefix"]))],2)):(t(),m("div",{key:1,class:b(`${S}-progress-text`),style:j({color:n}),role:"none"},[I("span",{class:b(`${S}-progress-text__percentage`)},[P(()=>_)],2),I("span",{class:b(`${S}-progress-text__unit`)},[P(()=>k)],2)],6))],64))])):P(()=>null)],2)}}});const Ar={success:(t(),x(ut)),error:(t(),x(dt)),warning:(t(),x(qe)),info:(t(),x(lt))};var Or=U({name:"ProgressLine",props:{clsPrefix:{type:String,required:!0},percentage:{type:Number,default:0},railColor:String,railStyle:[String,Object],fillColor:[String,Object],status:{type:String,required:!0},indicatorPlacement:{type:String,required:!0},indicatorTextColor:String,unit:{type:String,default:"%"},processing:{type:Boolean,required:!0},showIndicator:{type:Boolean,required:!0},height:[String,Number],railBorderRadius:[String,Number],fillBorderRadius:[String,Number]},setup(e,{slots:r}){const l=D(()=>me(e.height)),d=D(()=>typeof e.fillColor=="object"?`linear-gradient(to right, ${e.fillColor?.stops[0]} , ${e.fillColor?.stops[1]})`:e.fillColor),c=D(()=>e.railBorderRadius!==void 0?me(e.railBorderRadius):e.height!==void 0?me(e.height,{c:.5}):""),u=D(()=>e.fillBorderRadius!==void 0?me(e.fillBorderRadius):e.railBorderRadius!==void 0?me(e.railBorderRadius):e.height!==void 0?me(e.height,{c:.5}):"");return()=>{const{indicatorPlacement:p,railColor:g,railStyle:C,percentage:z,unit:_,indicatorTextColor:i,status:n,showIndicator:k,processing:w,clsPrefix:S}=e;return t(),m("div",{class:b(`${S}-progress-content`),role:"none"},[I("div",{class:b(`${S}-progress-graph`),"aria-hidden":!0},[I("div",{class:b([`${S}-progress-graph-line`,{[`${S}-progress-graph-line--indicator-${p}`]:!0}])},[I("div",{class:b(`${S}-progress-graph-line-rail`),style:j([{backgroundColor:g,height:l.value,borderRadius:c.value},C])},[I("div",{class:b([`${S}-progress-graph-line-fill`,w&&`${S}-progress-graph-line-fill--processing`]),style:j({maxWidth:`${e.percentage}%`,background:d.value,height:l.value,lineHeight:l.value,borderRadius:u.value})},[p==="inside"?(t(),m("div",{key:0,class:b(`${S}-progress-graph-line-indicator`),style:j({color:i})},[r.default?(t(),m(q,{key:0},[P(()=>r.default())],64)):(t(),m(q,{key:1},[P(()=>`${z}${_}`)],64))],6)):P(()=>null)],6)],6)],2)],2),k&&p==="outside"?(t(),m("div",{key:0},[r.default?(t(),m("div",{key:0,class:b(`${S}-progress-custom-content`),style:j({color:i}),role:"none"},[P(()=>r.default())],6)):(t(),m(q,{key:1},[n==="default"?(t(),m("div",{key:0,role:"none",class:b(`${S}-progress-icon ${S}-progress-icon--as-text`),style:j({color:i})},[P(()=>z),P(()=>_)],6)):(t(),m("div",{key:1,class:b(`${S}-progress-icon`),"aria-hidden":!0},[(t(),x(he,{clsPrefix:S},{default:()=>Ar[n]},1032,["clsPrefix"]))],2))],64))])):P(()=>null)],2)}}});const Ur=["id"],Fr=["stop-color"],jr=["stop-color"],qr=["d","stroke-width"],Wr=["d","stroke-width"],Er=["viewBox"];function et(e,r,l=100){return`m ${l/2} ${l/2-e} a ${e} ${e} 0 1 1 0 ${2*e} a ${e} ${e} 0 1 1 0 -${2*e}`}var Hr=U({name:"ProgressMultipleCircle",props:{clsPrefix:{type:String,required:!0},viewBoxWidth:{type:Number,required:!0},percentage:{type:Array,default:[0]},strokeWidth:{type:Number,required:!0},circleGap:{type:Number,required:!0},showIndicator:{type:Boolean,required:!0},fillColor:{type:Array,default:()=>[]},railColor:{type:Array,default:()=>[]},railStyle:{type:Array,default:()=>[]}},setup(e,{slots:r}){const l=D(()=>e.percentage.map((c,u)=>`${Math.PI*c/100*(e.viewBoxWidth/2-e.strokeWidth/2*(1+2*u)-e.circleGap*u)*2}, ${e.viewBoxWidth*8}`)),d=(c,u)=>{const p=e.fillColor[u],g=typeof p=="object"?p.stops[0]:"",C=typeof p=="object"?p.stops[1]:"";return typeof e.fillColor[u]=="object"&&(t(),m("linearGradient",{id:`gradient-${u}`,x1:"100%",y1:"0%",x2:"0%",y2:"100%"},[I("stop",{offset:"0%","stop-color":g},null,8,Fr),I("stop",{offset:"100%","stop-color":C},null,8,jr)],8,Ur))};return()=>{const{viewBoxWidth:c,strokeWidth:u,circleGap:p,showIndicator:g,fillColor:C,railColor:z,railStyle:_,percentage:i,clsPrefix:n}=e;return t(),m("div",{class:b(`${n}-progress-content`),role:"none"},[I("div",{class:b(`${n}-progress-graph`),"aria-hidden":!0},[I("div",{class:b(`${n}-progress-graph-circle`)},[(t(),m("svg",{viewBox:`0 0 ${c} ${c}`},[I("defs",null,[P(()=>i.map((k,w)=>d(k,w)))]),P(()=>i.map((k,w)=>(t(),m("g",{key:w},[I("path",{class:b(`${n}-progress-graph-circle-rail`),d:et(c/2-u/2*(1+2*w)-p*w,u,c),"stroke-width":u,"stroke-linecap":"round",fill:"none",style:j([{strokeDashoffset:0,stroke:z[w]},_[w]])},null,14,qr),I("path",{class:b([`${n}-progress-graph-circle-fill`,k===0&&`${n}-progress-graph-circle-fill--empty`]),d:et(c/2-u/2*(1+2*w)-p*w,u,c),"stroke-width":u,"stroke-linecap":"round",fill:"none",style:j({strokeDasharray:l.value[w],strokeDashoffset:0,stroke:typeof C[w]=="object"?`url(#gradient-${w})`:C[w]})},null,14,Wr)]))))],8,Er))],2)],2),g&&r.default?(t(),m("div",{key:0},[I("div",{class:b(`${n}-progress-text`)},[P(()=>r.default())],2)])):P(()=>null)],2)}}}),Kr=A([y("progress",{display:"inline-block"},[y("progress-icon",`
 color: var(--n-icon-color);
 transition: color .3s var(--n-bezier);
 `),Z("line",`
 width: 100%;
 display: block;
 `,[y("progress-content",`
 display: flex;
 align-items: center;
 `,[y("progress-graph",{flex:1})]),y("progress-custom-content",{marginLeft:"14px"}),y("progress-icon",`
 width: 30px;
 padding-left: 14px;
 height: var(--n-icon-size-line);
 line-height: var(--n-icon-size-line);
 font-size: var(--n-icon-size-line);
 `,[Z("as-text",`
 color: var(--n-text-color-line-outer);
 text-align: center;
 width: 40px;
 font-size: var(--n-font-size);
 padding-left: 4px;
 transition: color .3s var(--n-bezier);
 `)])]),Z("circle, dashboard",{width:"120px"},[y("progress-custom-content",`
 position: absolute;
 left: 50%;
 top: 50%;
 transform: translateX(-50%) translateY(-50%);
 display: flex;
 align-items: center;
 justify-content: center;
 `),y("progress-text",`
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
 `),y("progress-icon",`
 position: absolute;
 left: 50%;
 top: 50%;
 transform: translateX(-50%) translateY(-50%);
 display: flex;
 align-items: center;
 color: var(--n-icon-color);
 font-size: var(--n-icon-size-circle);
 `)]),Z("multiple-circle",`
 width: 200px;
 color: inherit;
 `,[y("progress-text",`
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
 `)]),y("progress-content",{position:"relative"}),y("progress-graph",{position:"relative"},[y("progress-graph-circle",[A("svg",{verticalAlign:"bottom"}),y("progress-graph-circle-fill",`
 stroke: var(--n-fill-color);
 transition:
 opacity .3s var(--n-bezier),
 stroke .3s var(--n-bezier),
 stroke-dasharray .3s var(--n-bezier);
 `,[Z("empty",{opacity:0})]),y("progress-graph-circle-rail",`
 transition: stroke .3s var(--n-bezier);
 overflow: hidden;
 stroke: var(--n-rail-color);
 `)]),y("progress-graph-line",[Z("indicator-inside",[y("progress-graph-line-rail",`
 height: 16px;
 line-height: 16px;
 border-radius: 10px;
 `,[y("progress-graph-line-fill",`
 height: inherit;
 border-radius: 10px;
 `),y("progress-graph-line-indicator",`
 background: #0000;
 white-space: nowrap;
 text-align: right;
 margin-left: 14px;
 margin-right: 14px;
 height: inherit;
 font-size: 12px;
 color: var(--n-text-color-line-inner);
 transition: color .3s var(--n-bezier);
 `)])]),Z("indicator-inside-label",`
 height: 16px;
 display: flex;
 align-items: center;
 `,[y("progress-graph-line-rail",`
 flex: 1;
 transition: background-color .3s var(--n-bezier);
 `),y("progress-graph-line-indicator",`
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
 `)]),y("progress-graph-line-rail",`
 position: relative;
 overflow: hidden;
 height: var(--n-rail-height);
 border-radius: 5px;
 background-color: var(--n-rail-color);
 transition: background-color .3s var(--n-bezier);
 `,[y("progress-graph-line-fill",`
 background: var(--n-fill-color);
 position: relative;
 border-radius: 5px;
 height: inherit;
 width: 100%;
 max-width: 0%;
 transition:
 background-color .3s var(--n-bezier),
 max-width .2s var(--n-bezier);
 `,[Z("processing",[A("&::after",`
 content: "";
 background-image: var(--n-line-bg-processing);
 animation: progress-processing-animation 2s var(--n-bezier) infinite;
 `)])])])])])]),A("@keyframes progress-processing-animation",`
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
 `)]);const Lr=["aria-valuenow","role"],Gr={...de.props,processing:Boolean,type:{type:String,default:"line"},gapDegree:Number,gapOffsetDegree:Number,status:{type:String,default:"default"},railColor:[String,Array],railStyle:[String,Array],color:[String,Array,Object],viewBoxWidth:{type:Number,default:100},strokeWidth:{type:Number,default:7},percentage:[Number,Array],unit:{type:String,default:"%"},showIndicator:{type:Boolean,default:!0},indicatorPosition:{type:String,default:"outside"},indicatorPlacement:{type:String,default:"outside"},indicatorTextColor:String,circleGap:{type:Number,default:1},height:Number,borderRadius:[String,Number],fillBorderRadius:[String,Number],offsetDegree:Number};var Xr=U({name:"Progress",props:Gr,setup(e){const r=D(()=>e.indicatorPlacement||e.indicatorPosition),l=D(()=>{if(e.gapDegree||e.gapDegree===0)return e.gapDegree;if(e.type==="dashboard")return 75}),{mergedClsPrefixRef:d,inlineThemeDisabled:c}=ye(e),u=de("Progress","-progress",Kr,Et,e,d),p=D(()=>{const{status:C}=e,{common:{cubicBezierEaseInOut:z},self:{fontSize:_,fontSizeCircle:i,railColor:n,railHeight:k,iconSizeCircle:w,iconSizeLine:S,textColorCircle:f,textColorLineInner:V,textColorLineOuter:N,lineBgProcessing:F,fontWeightCircle:L,[G("iconColor",C)]:W,[G("fillColor",C)]:E}}=u.value;return{"--n-bezier":z,"--n-fill-color":E,"--n-font-size":_,"--n-font-size-circle":i,"--n-font-weight-circle":L,"--n-icon-color":W,"--n-icon-size-circle":w,"--n-icon-size-line":S,"--n-line-bg-processing":F,"--n-rail-color":n,"--n-rail-height":k,"--n-text-color-circle":f,"--n-text-color-line-inner":V,"--n-text-color-line-outer":N}}),g=c?We("progress",D(()=>e.status[0]),p,e):void 0;return{mergedClsPrefix:d,mergedIndicatorPlacement:r,gapDeg:l,cssVars:c?void 0:p,themeClass:g?.themeClass,onRender:g?.onRender}},render(){const{type:e,cssVars:r,indicatorTextColor:l,showIndicator:d,status:c,railColor:u,railStyle:p,color:g,percentage:C,viewBoxWidth:z,strokeWidth:_,mergedIndicatorPlacement:i,unit:n,borderRadius:k,fillBorderRadius:w,height:S,processing:f,circleGap:V,mergedClsPrefix:N,gapDeg:F,gapOffsetDegree:L,themeClass:W,$slots:E,onRender:ne}=this;return ne?.(),t(),m("div",{class:b([W,`${N}-progress`,`${N}-progress--${e}`,`${N}-progress--${c}`]),style:j(r),"aria-valuemax":100,"aria-valuemin":0,"aria-valuenow":C,role:e==="circle"||e==="line"||e==="dashboard"?"progressbar":"none"},[e==="circle"||e==="dashboard"?(t(),x(Vr,{key:0,clsPrefix:N,status:c,showIndicator:d,indicatorTextColor:l,railColor:u,fillColor:g,railStyle:p,offsetDegree:this.offsetDegree,percentage:C,viewBoxWidth:z,strokeWidth:_,gapDegree:F===void 0?e==="dashboard"?75:0:F,gapOffsetDegree:L,unit:n},Pe(E),1032,["clsPrefix","status","showIndicator","indicatorTextColor","railColor","fillColor","railStyle","offsetDegree","percentage","viewBoxWidth","strokeWidth","gapDegree","gapOffsetDegree","unit"])):(t(),m(q,{key:1},[e==="line"?(t(),x(Or,{key:0,clsPrefix:N,status:c,showIndicator:d,indicatorTextColor:l,railColor:u,fillColor:g,railStyle:p,percentage:C,processing:f,indicatorPlacement:i,unit:n,fillBorderRadius:w,railBorderRadius:k,height:S},Pe(E),1032,["clsPrefix","status","showIndicator","indicatorTextColor","railColor","fillColor","railStyle","percentage","processing","indicatorPlacement","unit","fillBorderRadius","railBorderRadius","height"])):(t(),m(q,{key:1},[e==="multiple-circle"?(t(),x(Hr,{key:0,clsPrefix:N,strokeWidth:_,railColor:u,fillColor:g,railStyle:p,viewBoxWidth:z,percentage:C,showIndicator:d,circleGap:V},Pe(E),1032,["clsPrefix","strokeWidth","railColor","fillColor","railStyle","viewBoxWidth","percentage","showIndicator","circleGap"])):P(()=>null)],64))],64))],14,Lr)}}),Yr=y("steps",`
 width: 100%;
 display: flex;
`,[y("step",`
 position: relative;
 display: flex;
 flex: 1;
 `,[Z("disabled","cursor: not-allowed"),Z("clickable",`
 cursor: pointer;
 `),A("&:last-child",[y("step-splitor","display: none;")])]),y("step-splitor",`
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
 `),y("step-content","flex: 1;",[y("step-content-header",`
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
 `,[pe("title",`
 white-space: nowrap;
 flex: 0;
 `)]),pe("description",`
 color: var(--n-description-text-color);
 margin-top: 12px;
 margin-left: 9px;
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier);
 `)]),y("step-indicator",`
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
 `,[y("step-indicator-slot",`
 position: relative;
 width: var(--n-indicator-icon-size);
 height: var(--n-indicator-icon-size);
 font-size: var(--n-indicator-icon-size);
 line-height: var(--n-indicator-icon-size);
 `,[pe("index",`
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
 `,[Ve()]),y("icon",`
 color: var(--n-indicator-text-color);
 transition: color .3s var(--n-bezier);
 `,[Ve()]),y("base-icon",`
 color: var(--n-indicator-text-color);
 transition: color .3s var(--n-bezier);
 `,[Ve()])])]),Z("vertical","flex-direction: column;",[Ge("show-description",[A(">",[y("step","padding-bottom: 8px;")])]),A(">",[y("step","margin-bottom: 16px;",[A("&:last-child","margin-bottom: 0;"),A(">",[y("step-indicator",[A(">",[y("step-splitor",`
 position: absolute;
 bottom: -8px;
 width: 1px;
 margin: 0 !important;
 left: calc(var(--n-indicator-size) / 2);
 height: calc(100% - var(--n-indicator-size));
 `)])]),y("step-content",[pe("description","margin-top: 8px;")])])])])]),Z("content-bottom",[Ge("vertical",[A(">",[y("step","flex-direction: column",[A(">",[y("step-line","display: flex;",[A(">",[y("step-splitor",`
 margin-top: 0;
 align-self: center;
 `)])])]),A(">",[y("step-content","margin-top: calc(var(--n-indicator-size) / 2 - var(--n-step-header-font-size) / 2);",[y("step-content-header",`
 margin-left: 0;
 `),y("step-content__description",`
 margin-left: 0;
 `)])])])])])])]);function Zr(e,r){return typeof e!="object"||e===null||Array.isArray(e)?null:(e.props||(e.props={}),e.props.internalIndex=r+1,e)}function Jr(e){return e.map((r,l)=>Zr(r,l))}const Qr={...de.props,current:Number,status:{type:String,default:"process"},size:{type:String,default:"medium"},vertical:Boolean,contentPlacement:{type:String,default:"right"},"onUpdate:current":[Function,Array],onUpdateCurrent:[Function,Array]},mt=ot("n-steps");var ei=U({name:"Steps",props:Qr,slots:Object,setup(e,{slots:r}){const{mergedClsPrefixRef:l,mergedRtlRef:d}=ye(e),c=nt("Steps",d,l),u=de("Steps","-steps",Yr,Kt,e,l);return st(mt,{props:e,mergedThemeRef:u,mergedClsPrefixRef:l,stepsSlots:r}),{mergedClsPrefix:l,rtlEnabled:c}},render(){const{mergedClsPrefix:e}=this;return t(),m("div",{class:b([`${e}-steps`,this.rtlEnabled&&`${e}-steps--rtl`,this.vertical&&`${e}-steps--vertical`,this.contentPlacement==="bottom"&&`${e}-steps--content-bottom`])},[P(()=>Jr(Ht(lr(this))))],2)}});const ti=["onClick"],ri={status:String,title:String,description:String,disabled:Boolean,internalIndex:{type:Number,default:0}};var Ue=U({name:"Step",props:ri,slots:Object,setup(e){const r=at(mt,null);r||Gt("step","`n-step` must be placed inside `n-steps`.");const{inlineThemeDisabled:l}=ye(),{props:d,mergedThemeRef:c,mergedClsPrefixRef:u,stepsSlots:p}=r,g=we(d,"vertical"),C=we(d,"contentPlacement"),z=D(()=>{const{status:n}=e;if(n)return n;{const{internalIndex:k}=e,{current:w}=d;if(w===void 0)return"process";if(k<w)return"finish";if(k===w)return d.status||"process";if(k>w)return"wait"}return"process"}),_=D(()=>{const{value:n}=z,{size:k}=d,{common:{cubicBezierEaseInOut:w},self:{stepHeaderFontWeight:S,[G("stepHeaderFontSize",k)]:f,[G("indicatorIndexFontSize",k)]:V,[G("indicatorSize",k)]:N,[G("indicatorIconSize",k)]:F,[G("indicatorTextColor",n)]:L,[G("indicatorBorderColor",n)]:W,[G("headerTextColor",n)]:E,[G("splitorColor",n)]:ne,[G("indicatorColor",n)]:J,[G("descriptionTextColor",n)]:re}}=c.value;return{"--n-bezier":w,"--n-description-text-color":re,"--n-header-text-color":E,"--n-indicator-border-color":W,"--n-indicator-color":J,"--n-indicator-icon-size":F,"--n-indicator-index-font-size":V,"--n-indicator-size":N,"--n-indicator-text-color":L,"--n-splitor-color":ne,"--n-step-header-font-size":f,"--n-step-header-font-weight":S}}),i=l?We("step",D(()=>{const{value:n}=z,{size:k}=d;return`${n[0]}${k[0]}`}),_,d):void 0;return{stepsSlots:p,mergedClsPrefix:u,vertical:g,mergedStatus:z,handleStepClick:D(()=>{if(e.disabled)return;const{onUpdateCurrent:n,"onUpdate:current":k}=d;return n||k?()=>{n&&te(n,e.internalIndex),k&&te(k,e.internalIndex)}:void 0}),cssVars:l?void 0:_,themeClass:i?.themeClass,onRender:i?.onRender,contentPlacement:C}},render(){const{mergedClsPrefix:e,onRender:r,handleStepClick:l,disabled:d,contentPlacement:c,vertical:u}=this,p=ve(this.$slots.default,i=>{const n=i||this.description;return n?(t(),m("div",{key:1,class:b(`${e}-step-content__description`)},[P(()=>n)],2)):null}),g=(t(),m("div",{class:b(`${e}-step-splitor`)},null,2)),C=(t(),m("div",{class:b(`${e}-step-indicator`),key:c},[I("div",{class:b(`${e}-step-indicator-slot`)},[v(Lt,null,{default:()=>ve(this.$slots.icon,i=>{const{mergedStatus:n,stepsSlots:k}=this;return n==="finish"||n==="error"?n==="finish"?(t(),x(he,{clsPrefix:e,key:"finish"},{default:()=>ge(k["finish-icon"],()=>[(t(),x(or))])},1032,["clsPrefix"])):n==="error"?(t(),x(he,{clsPrefix:e,key:"error"},{default:()=>ge(k["error-icon"],()=>[(t(),x(Xt))])},1032,["clsPrefix"])):null:i||(t(),m("div",{key:this.internalIndex,class:b(`${e}-step-indicator-slot__index`)},[P(()=>this.internalIndex)],2))})},1024)],2),u?(t(),m(q,{key:0},[P(()=>g)],64)):P(()=>null)],2)),z=(t(),m("div",{class:b(`${e}-step-content`)},[I("div",{class:b(`${e}-step-content-header`)},[I("div",{class:b(`${e}-step-content-header__title`)},[P(()=>ge(this.$slots.title,()=>[this.title]))],2),!u&&c==="right"?(t(),m(q,{key:0},[P(()=>g)],64)):P(()=>null)],2),P(()=>p)],2));let _;return!u&&c==="bottom"?_=(i=>(t(),m(q,{key:5},[I("div",{class:b(`${e}-step-line`)},[P(()=>C),P(()=>g)],2),P(()=>z)],64)))():_=(i=>(t(),m(q,{key:6},[P(()=>C),P(()=>z)],64)))(),r?.(),t(),m("div",{class:b([`${e}-step`,d&&`${e}-step--disabled`,!d&&l&&`${e}-step--clickable`,this.themeClass,p&&`${e}-step--show-description`,`${e}-step--${this.mergedStatus}-status`]),style:j(this.cssVars),onClick:l},[P(()=>_)],14,ti)}});const vt=U({__name:"ServerStatusTag",props:{status:{},size:{default:"small"}},setup(e){const r={pending:"default",validating:"info",ready:"success",offline:"warning",error:"error"},l={pending:"Pending",validating:"Validating",ready:"Ready",offline:"Offline",error:"Error"},d=e,c=D(()=>r[d.status]??"default"),u=D(()=>l[d.status]??d.status);return(p,g)=>(t(),x(s(ft),{type:c.value,size:e.size,round:""},{default:h(()=>[R(ie(u.value),1)]),_:1},8,["type","size"]))}}),ii="—";function tt(e){if(e==null||Number.isNaN(e))return ii;if(e<=0)return"0 B";const r=["B","KiB","MiB","GiB","TiB","PiB"],l=Math.min(Math.floor(Math.log(e)/Math.log(1024)),r.length-1),d=e/1024**l;let c=0;return l>0&&(c=d>=100?1:2),`${d.toFixed(c)} ${r[l]}`}function ni(e){if(!e)return"never";const r=new Date(e).getTime();if(Number.isNaN(r))return"unknown";const l=Math.round((Date.now()-r)/1e3);if(l<45)return"just now";const d=Math.round(l/60);if(d<60)return`${d}m ago`;const c=Math.round(d/60);if(c<24)return`${c}h ago`;const u=Math.round(c/24);if(u<30)return`${u}d ago`;const p=Math.round(u/30);return p<12?`${p}mo ago`:`${Math.round(p/12)}y ago`}const oi="https://github.com/justindeelux/gotham/releases/latest/download",ai=U({__name:"AddServerWizard",props:{show:{type:Boolean}},emits:["update:show","created"],setup(e,{emit:r}){const l=e,d=r,c=pt(),u=ct(),p=`curl -fsSL ${oi}/install-agent.sh | sudo sh`,g=O(0),C=O(null),z=O(!1),_=O(!1),i=O(""),n=O(""),k=O(!1),w=O([]),S=O(null),f=Qt({name:"",ip:"",port:22,sshUser:"root",keyMode:"new",keyName:"",privateKey:"",keyId:""}),V=D(()=>({name:{required:!0,message:"Name is required",trigger:["input","blur"]},ip:[{required:!0,message:"IP address is required",trigger:["input","blur"]},{validator:(M,o)=>L(o),message:"Enter a valid IPv4 address or hostname",trigger:["input","blur"]}],port:{type:"number",required:!0,message:"Port is required",trigger:["input","blur"]},sshUser:{required:!0,message:"SSH user is required",trigger:["input","blur"]},keyName:f.keyMode==="new"?{required:!0,message:"Key name is required",trigger:["input","blur"]}:[],privateKey:f.keyMode==="new"?{required:!0,message:"Private key is required",trigger:["input","blur"]}:[],keyId:f.keyMode==="existing"?{required:!0,message:"Key ID is required",trigger:["input","blur"]}:[]})),N=D(()=>{const M=S.value;return M?c.servers.find(o=>o.id===M.id)??M:null}),F=D(()=>N.value?.status==="ready");it(g,M=>{M===1&&S.value&&w.value.length===0&&E()});function L(M){const o=M.trim();if(o==="")return!1;const B=/^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/,ze=/^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/;return B.test(o)||ze.test(o)}async function W(){i.value="";try{await C.value?.validate()}catch{return}z.value=!0;try{let M=null;f.keyMode==="new"?M=(await cr({name:f.keyName.trim(),private_key:f.privateKey})).id:M=f.keyId.trim()||null;const o=await c.addServer({name:f.name.trim(),ip:f.ip.trim(),port:f.port??22,ssh_user:f.sshUser.trim(),ssh_key_id:M});S.value=o,d("created",o),g.value=1}catch(M){i.value=$e(M)}finally{z.value=!1}}async function E(){const M=S.value;if(M){_.value=!0,n.value="";try{const o=await c.validate(M.id);w.value=o.checks,n.value=o.message,k.value=o.ok,o.ok&&u.success("Validation passed")}catch(o){n.value=$e(o)}finally{_.value=!1}}}async function ne(){try{await navigator.clipboard.writeText(p),u.success("Install command copied")}catch{u.error("Could not copy to clipboard")}}function J(){d("update:show",!1),H()}function re(M){d("update:show",M),M||H()}function H(){g.value=0,f.name="",f.ip="",f.port=22,f.sshUser="root",f.keyMode="new",f.keyName="",f.privateKey="",f.keyId="",i.value="",n.value="",k.value=!1,w.value=[],S.value=null,C.value?.restoreValidation()}return(M,o)=>(t(),x(s(Yt),{show:l.show,preset:"card",title:"Add server","mask-closable":!1,class:"wizard",style:{width:"680px","max-width":"94vw"},"onUpdate:show":re},{footer:h(()=>[v(s(Y),{justify:"end",size:8},{default:h(()=>[g.value===0?(t(),m(q,{key:0},[v(s(Q),{onClick:J},{default:h(()=>[...o[21]||(o[21]=[R("Cancel",-1)])]),_:1}),v(s(Q),{type:"primary",loading:z.value,onClick:W},{default:h(()=>[...o[22]||(o[22]=[R(" Create & continue ",-1)])]),_:1},8,["loading"])],64)):g.value===1?(t(),m(q,{key:1},[v(s(Q),{loading:_.value,onClick:E},{default:h(()=>[...o[23]||(o[23]=[R(" Retry validation ",-1)])]),_:1},8,["loading"]),v(s(Q),{type:"primary",disabled:!k.value,onClick:o[8]||(o[8]=B=>g.value=2)},{default:h(()=>[...o[24]||(o[24]=[R(" Continue ",-1)])]),_:1},8,["disabled"])],64)):(t(),x(s(Q),{key:2,type:"primary",onClick:J},{default:h(()=>[...o[25]||(o[25]=[R("Done",-1)])]),_:1}))]),_:1})]),default:h(()=>[v(s(Y),{vertical:"",size:20},{default:h(()=>[v(s(ei),{current:g.value+1,size:"small"},{default:h(()=>[v(s(Ue),{title:"Connection",description:"Host and credentials"}),v(s(Ue),{title:"Validate",description:"Probe the node"}),v(s(Ue),{title:"Install",description:"Install the agent"})]),_:1},8,["current"]),i.value?(t(),x(s(ke),{key:0,type:"error","show-icon":!0},{default:h(()=>[R(ie(i.value),1)]),_:1})):se("",!0),g.value===0?(t(),x(s(pr),{key:1,ref_key:"formRef",ref:C,model:f,rules:V.value,"label-placement":"top",onSubmit:Zt(W,["prevent"])},{default:h(()=>[v(s(Y),{vertical:"",size:4},{default:h(()=>[v(s(ae),{label:"Name",path:"name"},{default:h(()=>[v(s(le),{value:f.name,"onUpdate:value":o[0]||(o[0]=B=>f.name=B),placeholder:"web-1"},null,8,["value"])]),_:1}),v(s(Y),{size:12},{default:h(()=>[v(s(ae),{label:"IP address",path:"ip",class:"grow"},{default:h(()=>[v(s(le),{value:f.ip,"onUpdate:value":o[1]||(o[1]=B=>f.ip=B),placeholder:"10.0.0.5"},null,8,["value"])]),_:1}),v(s(ae),{label:"Port",path:"port",style:{width:"120px"}},{default:h(()=>[v(s(Sr),{value:f.port,"onUpdate:value":o[2]||(o[2]=B=>f.port=B),min:1,max:65535,placeholder:"22"},null,8,["value"])]),_:1})]),_:1}),v(s(ae),{label:"SSH user",path:"sshUser"},{default:h(()=>[v(s(le),{value:f.sshUser,"onUpdate:value":o[3]||(o[3]=B=>f.sshUser=B),placeholder:"root"},null,8,["value"])]),_:1}),v(s(ae),{label:"SSH key"},{default:h(()=>[v(s(ar),{value:f.keyMode,"onUpdate:value":o[4]||(o[4]=B=>f.keyMode=B),size:"small"},{default:h(()=>[v(s(Xe),{value:"new"},{default:h(()=>[...o[9]||(o[9]=[R("Paste a new key",-1)])]),_:1}),v(s(Xe),{value:"existing"},{default:h(()=>[...o[10]||(o[10]=[R("Use an existing key ID",-1)])]),_:1})]),_:1},8,["value"])]),_:1}),f.keyMode==="new"?(t(),m(q,{key:0},[v(s(ae),{label:"Key name",path:"keyName"},{default:h(()=>[v(s(le),{value:f.keyName,"onUpdate:value":o[5]||(o[5]=B=>f.keyName=B),placeholder:"deploy-key"},null,8,["value"])]),_:1}),v(s(ae),{label:"Private key (PEM)",path:"privateKey"},{default:h(()=>[v(s(le),{value:f.privateKey,"onUpdate:value":o[6]||(o[6]=B=>f.privateKey=B),type:"textarea",autosize:{minRows:4,maxRows:10},placeholder:"-----BEGIN OPENSSH PRIVATE KEY-----"},null,8,["value"])]),_:1}),v(s(X),{depth:"3"},{default:h(()=>[...o[11]||(o[11]=[R(" The key is encrypted at rest by the control plane. It is never returned by the API. ",-1)])]),_:1})],64)):(t(),x(s(ae),{key:1,label:"Key ID",path:"keyId"},{default:h(()=>[v(s(le),{value:f.keyId,"onUpdate:value":o[7]||(o[7]=B=>f.keyId=B),placeholder:"00000000-0000-0000-0000-000000000000"},null,8,["value"])]),_:1})),v(s(X),{depth:"3"},{default:h(()=>[...o[12]||(o[12]=[R(" Key listing is not exposed by the API yet, so paste the key material or an existing key ID. Password-auth servers are not supported in this release. ",-1)])]),_:1})]),_:1})]),_:1},8,["model","rules"])):g.value===1?(t(),x(s(Y),{key:2,vertical:"",size:12},{default:h(()=>[v(s(X),{depth:"2"},{default:h(()=>[...o[13]||(o[13]=[R(" Running readiness probes over SSH. Docker must be installed and reachable. ",-1)])]),_:1}),n.value&&!k.value?(t(),x(s(ke),{key:0,type:"error","show-icon":!0},{default:h(()=>[R(ie(n.value),1)]),_:1})):se("",!0),w.value.length?(t(),x(s(Y),{key:1,vertical:"",size:8},{default:h(()=>[(t(!0),m(q,null,Jt(w.value,B=>(t(),m("div",{key:B.name,class:"check-row"},[v(s(ft),{type:B.ok?"success":"error",size:"small",round:""},{default:h(()=>[R(ie(B.ok?"ok":"fail"),1)]),_:2},1032,["type"]),v(s(X),{strong:"",class:"check-name"},{default:h(()=>[R(ie(B.name.toUpperCase()),1)]),_:2},1024),v(s(X),{depth:"2",class:"check-detail"},{default:h(()=>[R(ie(B.detail),1)]),_:2},1024)]))),128))]),_:1})):_.value?se("",!0):(t(),x(s(X),{key:2,depth:"3"},{default:h(()=>[...o[14]||(o[14]=[R("No checks have run yet.",-1)])]),_:1})),k.value?(t(),x(s(ke),{key:3,type:"success","show-icon":!0},{default:h(()=>[...o[15]||(o[15]=[R(" All checks passed. Continue to install the node agent. ",-1)])]),_:1})):se("",!0)]),_:1})):(t(),x(s(Y),{key:3,vertical:"",size:12},{default:h(()=>[v(s(Y),{align:"center",size:8},{default:h(()=>[v(s(X),{depth:"2"},{default:h(()=>[...o[16]||(o[16]=[R("Current status:",-1)])]),_:1}),N.value?(t(),x(vt,{key:0,status:N.value.status},null,8,["status"])):se("",!0)]),_:1}),F.value?(t(),x(s(ke),{key:0,type:"success","show-icon":!0},{default:h(()=>[...o[17]||(o[17]=[R(" The agent registered and the server is ready. ",-1)])]),_:1})):se("",!0),v(s(X),{depth:"2"},{default:h(()=>[...o[18]||(o[18]=[R(" SSH into the node and run the installer. It downloads the agent, installs the systemd unit, and registers with the control plane. ",-1)])]),_:1}),v(s(Y),{align:"center",size:8},{default:h(()=>[v(s(le),{value:p,readonly:"",class:"grow"}),v(s(Q),{onClick:ne},{default:h(()=>[...o[19]||(o[19]=[R("Copy",-1)])]),_:1})]),_:1}),v(s(X),{depth:"3"},{default:h(()=>[...o[20]||(o[20]=[R(" The page keeps polling every 5 seconds; the status badge flips to Ready once the agent checks in. ",-1)])]),_:1}),N.value?(t(),x(s(X),{key:1,depth:"3"},{default:h(()=>[R(" Detected memory: "+ie(s(tt)(N.value.total_mem))+" · disk: "+ie(s(tt)(N.value.total_disk)),1)]),_:1})):se("",!0)]),_:1}))]),_:1})]),_:1},8,["show"]))}}),si=gr(ai,[["__scopeId","data-v-0ff1a4c0"]]),xi=U({__name:"ServersPage",setup(e){const r=pt(),l=ct(),d=O(!1),c=O(null);function u(i){return i==null?fe(X,{depth:3},{default:()=>"—"}):fe(Xr,{type:"line",percentage:Math.round(Math.min(Math.max(i,0),100)),height:14})}function p(i){return fe(Y,{size:8,align:"center",wrap:!1},{default:()=>[fe(Q,{size:"small",loading:c.value===i.id,onClick:()=>{z(i)}},{default:()=>"Validate"}),fe(_r,{onPositiveClick:()=>{_(i)}},{trigger:()=>fe(Q,{size:"small",type:"error",quaternary:!0},{default:()=>"Delete"}),default:()=>`Delete server "${i.name}"?`})]})}const g=[{title:"Name",key:"name",minWidth:140,ellipsis:{tooltip:!0}},{title:"Address",key:"address",minWidth:150,render:i=>`${i.ip}:${i.port}`},{title:"Status",key:"status",width:120,render:i=>fe(vt,{status:i.status})},{title:"CPU",key:"cpu_usage",width:140,render:i=>u(i.cpu_usage)},{title:"RAM",key:"mem_usage",width:140,render:i=>u(i.mem_usage)},{title:"Disk",key:"disk_usage",width:140,render:i=>u(i.disk_usage)},{title:"Docker",key:"docker_version",minWidth:120,render:i=>i.docker_version??"—"},{title:"Last seen",key:"last_seen",width:120,render:i=>ni(i.last_seen)},{title:"Actions",key:"actions",width:190,render:i=>p(i)}];function C(i){return i.id}async function z(i){c.value=i.id;try{const n=await r.validate(i.id);if(n.ok){l.success(`${i.name}: validation passed`);return}const k=n.checks.filter(w=>!w.ok).map(w=>w.name).join(", ");l.error(n.message||`${i.name}: failed checks: ${k}`)}catch(n){l.error($e(n))}finally{c.value=null}}async function _(i){try{await r.removeServer(i.id),l.success(`Deleted ${i.name}`)}catch(n){l.error($e(n))}}return er(()=>{r.fetchServers().catch(()=>{}),r.pollServers()}),tr(()=>{r.stopPolling()}),(i,n)=>(t(),x(s(Y),{vertical:"",size:16},{default:h(()=>[v(s(rr),null,{header:h(()=>[v(s(Y),{align:"center",justify:"space-between"},{default:h(()=>[v(s(X),{strong:""},{default:h(()=>[...n[2]||(n[2]=[R("Servers",-1)])]),_:1}),v(s(Q),{type:"primary",onClick:n[0]||(n[0]=k=>d.value=!0)},{default:h(()=>[...n[3]||(n[3]=[R(" Add server ",-1)])]),_:1})]),_:1})]),default:h(()=>[s(r).error?(t(),x(s(ke),{key:0,type:"error","show-icon":!0,style:{"margin-bottom":"12px"}},{default:h(()=>[R(ie(s(r).error),1)]),_:1})):se("",!0),v(s(sr),{columns:g,data:s(r).servers,loading:s(r).loading,"row-key":C,bordered:!1,pagination:{pageSize:10}},null,8,["data","loading"])]),_:1}),v(si,{show:d.value,"onUpdate:show":n[1]||(n[1]=k=>d.value=k)},null,8,["show"])]),_:1}))}});export{xi as default};
