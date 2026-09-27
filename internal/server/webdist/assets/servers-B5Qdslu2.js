import{G as ge,cr as ue,bC as r,I as ve,V as C,an as p,aX as z,H as I,d as Ce,J as G,T as A,o as x,c as B,M as f,a as be,S as k,l as pe,ax as fe,Z as L,N as ke,ae as me,a5 as xe,y as U,q as ye,at as Pe,P as d,bu as Se,bA as q,aP as ze,Y as Ie,aw as Be,az as m}from"./index-BQvpBbOo.js";function $e(e){const{textColor2:a,primaryColorHover:g,primaryColorPressed:b,primaryColor:l,infoColor:i,successColor:n,warningColor:s,errorColor:t,baseColor:u,borderColor:$,opacityDisabled:H,tagColor:R,closeIconColor:y,closeIconColorHover:P,closeIconColorPressed:o,borderRadiusSmall:c,fontSizeMini:v,fontSizeTiny:h,fontSizeSmall:M,fontSizeMedium:T,heightMini:_,heightTiny:w,heightSmall:E,heightMedium:W,closeColorHover:F,closeColorPressed:N,buttonColor2Hover:V,buttonColor2Pressed:j,fontWeightStrong:O}=e;return{...ue,closeBorderRadius:c,heightTiny:_,heightSmall:w,heightMedium:E,heightLarge:W,borderRadius:c,opacityDisabled:H,fontSizeTiny:v,fontSizeSmall:h,fontSizeMedium:M,fontSizeLarge:T,fontWeightStrong:O,textColorCheckable:a,textColorHoverCheckable:a,textColorPressedCheckable:a,textColorChecked:u,colorCheckable:"#0000",colorHoverCheckable:V,colorPressedCheckable:j,colorChecked:l,colorCheckedHover:g,colorCheckedPressed:b,border:`1px solid ${$}`,textColor:a,color:R,colorBordered:"rgb(250, 250, 252)",closeIconColor:y,closeIconColorHover:P,closeIconColorPressed:o,closeColorHover:F,closeColorPressed:N,borderPrimary:`1px solid ${r(l,{alpha:.3})}`,textColorPrimary:l,colorPrimary:r(l,{alpha:.12}),colorBorderedPrimary:r(l,{alpha:.1}),closeIconColorPrimary:l,closeIconColorHoverPrimary:l,closeIconColorPressedPrimary:l,closeColorHoverPrimary:r(l,{alpha:.12}),closeColorPressedPrimary:r(l,{alpha:.18}),borderInfo:`1px solid ${r(i,{alpha:.3})}`,textColorInfo:i,colorInfo:r(i,{alpha:.12}),colorBorderedInfo:r(i,{alpha:.1}),closeIconColorInfo:i,closeIconColorHoverInfo:i,closeIconColorPressedInfo:i,closeColorHoverInfo:r(i,{alpha:.12}),closeColorPressedInfo:r(i,{alpha:.18}),borderSuccess:`1px solid ${r(n,{alpha:.3})}`,textColorSuccess:n,colorSuccess:r(n,{alpha:.12}),colorBorderedSuccess:r(n,{alpha:.1}),closeIconColorSuccess:n,closeIconColorHoverSuccess:n,closeIconColorPressedSuccess:n,closeColorHoverSuccess:r(n,{alpha:.12}),closeColorPressedSuccess:r(n,{alpha:.18}),borderWarning:`1px solid ${r(s,{alpha:.35})}`,textColorWarning:s,colorWarning:r(s,{alpha:.15}),colorBorderedWarning:r(s,{alpha:.12}),closeIconColorWarning:s,closeIconColorHoverWarning:s,closeIconColorPressedWarning:s,closeColorHoverWarning:r(s,{alpha:.12}),closeColorPressedWarning:r(s,{alpha:.18}),borderError:`1px solid ${r(t,{alpha:.23})}`,textColorError:t,colorError:r(t,{alpha:.1}),colorBorderedError:r(t,{alpha:.08}),closeIconColorError:t,closeIconColorHoverError:t,closeIconColorPressedError:t,closeColorHoverError:r(t,{alpha:.12}),closeColorPressedError:r(t,{alpha:.18})}}const He={common:ge,self:$e};var Re={color:Object,type:{type:String,default:"default"},round:Boolean,size:String,closable:Boolean,disabled:{type:Boolean,default:void 0}},Me=ve("tag",`
 --n-close-margin: var(--n-close-margin-top) var(--n-close-margin-right) var(--n-close-margin-bottom) var(--n-close-margin-left);
 white-space: nowrap;
 position: relative;
 box-sizing: border-box;
 cursor: default;
 display: inline-flex;
 align-items: center;
 flex-wrap: nowrap;
 padding: var(--n-padding);
 border-radius: var(--n-border-radius);
 color: var(--n-text-color);
 background-color: var(--n-color);
 transition: 
 border-color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier),
 opacity .3s var(--n-bezier);
 line-height: 1;
 height: var(--n-height);
 font-size: var(--n-font-size);
`,[C("strong",`
 font-weight: var(--n-font-weight-strong);
 `),p("border",`
 pointer-events: none;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 border-radius: inherit;
 border: var(--n-border);
 transition: border-color .3s var(--n-bezier);
 `),p("icon",`
 display: flex;
 margin: 0 4px 0 0;
 color: var(--n-text-color);
 transition: color .3s var(--n-bezier);
 font-size: var(--n-avatar-size-override);
 `),p("avatar",`
 display: flex;
 margin: 0 6px 0 0;
 `),p("close",`
 margin: var(--n-close-margin);
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `),C("round",`
 padding: 0 calc(var(--n-height) / 3);
 border-radius: calc(var(--n-height) / 2);
 `,[p("icon",`
 margin: 0 4px 0 calc((var(--n-height) - 8px) / -2);
 `),p("avatar",`
 margin: 0 6px 0 calc((var(--n-height) - 8px) / -2);
 `),C("closable",`
 padding: 0 calc(var(--n-height) / 4) 0 calc(var(--n-height) / 3);
 `)]),C("icon, avatar",[C("round",`
 padding: 0 calc(var(--n-height) / 3) 0 calc(var(--n-height) / 2);
 `)]),C("disabled",`
 cursor: not-allowed !important;
 opacity: var(--n-opacity-disabled);
 `),C("checkable",`
 cursor: pointer;
 box-shadow: none;
 color: var(--n-text-color-checkable);
 background-color: var(--n-color-checkable);
 `,[z("disabled",[I("&:hover","background-color: var(--n-color-hover-checkable);",[z("checked","color: var(--n-text-color-hover-checkable);")]),I("&:active","background-color: var(--n-color-pressed-checkable);",[z("checked","color: var(--n-text-color-pressed-checkable);")])]),C("checked",`
 color: var(--n-text-color-checked);
 background-color: var(--n-color-checked);
 `,[z("disabled",[I("&:hover","background-color: var(--n-color-checked-hover);"),I("&:active","background-color: var(--n-color-checked-pressed);")])])])]);const Te=["onClick","onMouseenter","onMouseleave"],_e={...G.props,...Re,bordered:{type:Boolean,default:void 0},checked:Boolean,checkable:Boolean,strong:Boolean,triggerClickOnClose:Boolean,onClose:[Array,Function],onMouseenter:Function,onMouseleave:Function,"onUpdate:checked":Function,onUpdateChecked:Function,internalCloseFocusable:{type:Boolean,default:!0},internalCloseIsButtonTag:{type:Boolean,default:!0},onCheckedChange:Function},we=ze("n-tag");var Ne=Ce({name:"Tag",props:_e,slots:Object,setup(e){const a=ye(null),{mergedBorderedRef:g,mergedClsPrefixRef:b,inlineThemeDisabled:l,mergedRtlRef:i,mergedComponentPropsRef:n}=ke(e),s=U(()=>e.size||n?.value?.Tag?.size||"medium"),t=G("Tag","-tag",Me,He,e,b);Ie(we,{roundRef:Be(e,"round")});function u(){if(!e.disabled&&e.checkable){const{checked:o,onCheckedChange:c,onUpdateChecked:v,"onUpdate:checked":h}=e;v&&v(!o),h&&h(!o),c&&c(!o)}}function $(o){if(e.triggerClickOnClose||o.stopPropagation(),!e.disabled){const{onClose:c}=e;c&&Pe(c,o)}}const H={setTextContent(o){const{value:c}=a;c&&(c.textContent=o)}},R=me("Tag",i,b),y=U(()=>{const{type:o,color:{color:c,textColor:v}={}}=e,h=s.value,{common:{cubicBezierEaseInOut:M},self:{padding:T,closeMargin:_,borderRadius:w,opacityDisabled:E,textColorCheckable:W,textColorHoverCheckable:F,textColorPressedCheckable:N,textColorChecked:V,colorCheckable:j,colorHoverCheckable:O,colorPressedCheckable:J,colorChecked:X,colorCheckedHover:Y,colorCheckedPressed:Z,closeBorderRadius:Q,fontWeightStrong:ee,[d("colorBordered",o)]:oe,[d("closeSize",h)]:re,[d("closeIconSize",h)]:ae,[d("fontSize",h)]:le,[d("height",h)]:D,[d("color",o)]:se,[d("textColor",o)]:ce,[d("border",o)]:ne,[d("closeIconColor",o)]:K,[d("closeIconColorHover",o)]:te,[d("closeIconColorPressed",o)]:ie,[d("closeColorHover",o)]:de,[d("closeColorPressed",o)]:he}}=t.value,S=Se(_);return{"--n-font-weight-strong":ee,"--n-avatar-size-override":`calc(${D} - 8px)`,"--n-bezier":M,"--n-border-radius":w,"--n-border":ne,"--n-close-icon-size":ae,"--n-close-color-pressed":he,"--n-close-color-hover":de,"--n-close-border-radius":Q,"--n-close-icon-color":K,"--n-close-icon-color-hover":te,"--n-close-icon-color-pressed":ie,"--n-close-icon-color-disabled":K,"--n-close-margin-top":S.top,"--n-close-margin-right":S.right,"--n-close-margin-bottom":S.bottom,"--n-close-margin-left":S.left,"--n-close-size":re,"--n-color":c||(g.value?oe:se),"--n-color-checkable":j,"--n-color-checked":X,"--n-color-checked-hover":Y,"--n-color-checked-pressed":Z,"--n-color-hover-checkable":O,"--n-color-pressed-checkable":J,"--n-font-size":le,"--n-height":D,"--n-opacity-disabled":E,"--n-padding":T,"--n-text-color":v||ce,"--n-text-color-checkable":W,"--n-text-color-checked":V,"--n-text-color-hover-checkable":F,"--n-text-color-pressed-checkable":N}}),P=l?xe("tag",U(()=>{let o="";const{type:c,color:{color:v,textColor:h}={}}=e;return o+=c[0],o+=s.value[0],v&&(o+=`a${q(v)}`),h&&(o+=`b${q(h)}`),g.value&&(o+="c"),o}),y,e):void 0;return{...H,rtlEnabled:R,mergedClsPrefix:b,contentRef:a,mergedBordered:g,handleClick:u,handleCloseClick:$,cssVars:l?void 0:y,themeClass:P?.themeClass,onRender:P?.onRender}},render(){const{mergedClsPrefix:e,rtlEnabled:a,closable:g,color:{borderColor:b}={},round:l,onRender:i,$slots:n}=this;i?.();const s=A(n.avatar,u=>u&&(x(),B("div",{class:k(`${e}-tag__avatar`)},[f(()=>u)],2))),t=A(n.icon,u=>u&&(x(),B("div",{class:k(`${e}-tag__icon`)},[f(()=>u)],2)));return x(),B("div",{class:k([`${e}-tag`,this.themeClass,{[`${e}-tag--rtl`]:a,[`${e}-tag--strong`]:this.strong,[`${e}-tag--disabled`]:this.disabled,[`${e}-tag--checkable`]:this.checkable,[`${e}-tag--checked`]:this.checkable&&this.checked,[`${e}-tag--round`]:l,[`${e}-tag--avatar`]:s,[`${e}-tag--icon`]:t,[`${e}-tag--closable`]:g}]),style:L(this.cssVars),onClick:this.handleClick,onMouseenter:this.onMouseenter,onMouseleave:this.onMouseleave},[f(()=>t||s),be("span",{class:k(`${e}-tag__content`),ref:"contentRef"},[f(()=>this.$slots.default?.())],2),!this.checkable&&g?(x(),pe(fe,{key:0,clsPrefix:e,class:k(`${e}-tag__close`),disabled:this.disabled,onClick:this.handleCloseClick,focusable:this.internalCloseFocusable,round:l,isButtonTag:this.internalCloseIsButtonTag,absolute:!0},null,8,["clsPrefix","class","disabled","onClick","focusable","round","isButtonTag"])):f(()=>null),!this.checkable&&this.mergedBordered?(x(),B("div",{key:2,class:k(`${e}-tag__border`),style:L({borderColor:b})},null,6)):f(()=>null)],46,Te)}});const Ee=45e3;async function Ve(){return(await m.get("/servers")).data.servers??[]}async function je(e){return(await m.post("/servers",e)).data.server}async function Oe(e){return(await m.get(`/servers/${e}`)).data.server}async function Ue(e){await m.delete(`/servers/${e}`)}async function De(e){const a=await m.post(`/servers/${e}/validate`,{},{timeout:Ee,validateStatus:g=>g===200||g===422});return{ok:a.status===200,checks:a.data.checks??[],server:a.data.server??null,message:a.data.message??""}}async function Ke(e){return(await m.post("/private-keys",e)).data}function We(e){return typeof e=="object"&&e!==null&&"message"in e&&"status"in e}function Ae(e){return We(e)?e.message||"Request failed":e instanceof Error?e.message:"Something went wrong. Please try again."}export{Ne as T,je as a,Ue as b,Ke as c,Ae as d,Oe as g,We as i,Ve as l,we as t,De as v};
